package downloader

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	httpclient "github.com/redraw/rapel/internal/http"
)

// Config holds downloader configuration
type Config struct {
	URL                 string
	ChunkSize           int64
	MaxConcurrency      int
	Force               bool
	HTTPConfig          httpclient.Config
	TotalSize           int64  // Optional: if 0, will perform HEAD request
	PostPartCmd         string // Optional: command to run after each part completes
	PostPartConcurrency int    // Optional: max concurrent post-part commands (0 = unlimited)
}

// HasPostPartCmd returns whether post-part command is configured
func (c *Config) HasPostPartCmd() bool {
	return c.PostPartCmd != ""
}

// Downloader manages the chunked download process
type Downloader struct {
	config     Config
	args       *DownloadArguments
	client     *httpclient.Client
	progress   *ProgressTracker
	postPartWg sync.WaitGroup
	postPartCh chan int
}

// NewDownloader creates a new Downloader
func NewDownloader(config Config) (*Downloader, error) {
	client, err := httpclient.NewClient(config.HTTPConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Downloader{
		config: config,
		client: client,
	}, nil
}

// Download performs the chunked download
func (d *Downloader) Download(ctx context.Context) error {
	prefix := extractFilenameFromURL(d.config.URL)
	if prefix == "" {
		prefix = "download"
	}

	// Load existing args if not forcing a fresh start
	var existingArgs *DownloadArguments
	if !d.config.Force {
		var err error
		existingArgs, err = LoadDownloadArguments(prefix)
		if err != nil {
			return fmt.Errorf("failed to load args: %w", err)
		}
	}

	// Get content length if not provided
	totalSize := d.config.TotalSize
	if totalSize == 0 {
		var err error
		totalSize, err = d.client.GetContentLength(ctx, d.config.URL)
		if err != nil {
			return fmt.Errorf("failed to get content length: %w", err)
		}
	}

	// Validate loaded args or create fresh ones
	if existingArgs != nil && (existingArgs.URL != d.config.URL || existingArgs.TotalSize != totalSize) {
		if !d.config.Force {
			return fmt.Errorf("existing args don't match URL/size, use --force to restart")
		}
		existingArgs = nil
	}

	if existingArgs != nil {
		d.args = existingArgs
	} else {
		d.args = NewDownloadArguments(d.config.URL, totalSize, d.config.ChunkSize, prefix)
		if err := d.args.Save(); err != nil {
			return fmt.Errorf("failed to save args: %w", err)
		}
	}

	// Build progress tracker
	d.progress = NewProgressTracker(d.args)

	// Seed progress from on-disk chunk files (resume detection)
	for i := 0; i < d.args.NumChunks(); i++ {
		if _, err := os.Stat(d.args.PartPath(i)); err == nil {
			// .part exists: chunk is complete
			d.progress.MarkComplete(i)
		} else if info, err := os.Stat(d.args.TmpPath(i)); err == nil {
			// .tmp exists: partially downloaded; seed for display but don't mark complete
			size := info.Size()
			if size > d.args.ChunkSizeAt(i) {
				size = d.args.ChunkSizeAt(i)
			}
			d.progress.SeedChunk(i, size)
		}
	}

	fmt.Printf("URL        : %s\n", d.config.URL)
	fmt.Printf("File       : %s\n", prefix)
	fmt.Printf("Size       : %s\n", formatBytes(totalSize))
	fmt.Printf("Chunk size : %s\n", formatBytes(d.config.ChunkSize))
	fmt.Printf("Chunks     : %d\n", d.args.NumChunks())
	fmt.Printf("Jobs       : %d\n", d.config.MaxConcurrency)
	fmt.Println()

	if err := d.downloadAllChunks(ctx); err != nil {
		return err
	}

	d.progress.PrintComplete()

	if err := d.args.Delete(); err != nil {
		return fmt.Errorf("failed to delete args file: %w", err)
	}

	return nil
}

// downloadAllChunks downloads all chunks using a worker pool
func (d *Downloader) downloadAllChunks(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workChan := make(chan int)
	errChan := make(chan error, 1)

	if d.config.HasPostPartCmd() {
		d.postPartCh = make(chan int, d.args.NumChunks())
		d.startPostPartWorkers()
	}

	var wg sync.WaitGroup
	for i := 0; i < d.config.MaxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range workChan {
				if err := d.downloadChunk(ctx, index); err != nil {
					select {
					case errChan <- fmt.Errorf("chunk %d: %w", index, err):
						cancel()
					default:
					}
					return
				}

				d.progress.MarkComplete(index)
				d.progress.PrintChunkComplete(index)

				if d.config.HasPostPartCmd() {
					select {
					case d.postPartCh <- index:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Dispatch chunks: skip complete ones, send incomplete to workers
	go func() {
		defer close(workChan)
		for i := 0; i < d.args.NumChunks(); i++ {
			if d.progress.IsChunkComplete(i) {
				// Already done — enqueue post-part (at-least-once on resume)
				if d.config.HasPostPartCmd() {
					select {
					case d.postPartCh <- i:
					case <-ctx.Done():
						return
					}
				}
				continue
			}

			select {
			case workChan <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	close(errChan)

	if d.config.HasPostPartCmd() {
		close(d.postPartCh)
		d.progress.PrintMessage("Waiting for post-part commands to complete...")
		d.postPartWg.Wait()
	}

	if err := <-errChan; err != nil {
		return err
	}

	return nil
}

// downloadChunk downloads a single chunk with resume support and retry logic
func (d *Downloader) downloadChunk(ctx context.Context, index int) error {
	start, end := d.args.ChunkRange(index)
	tmpPath := d.args.TmpPath(index)
	partPath := d.args.PartPath(index)

	var lastErr error
	maxRetries := d.config.HTTPConfig.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoffSecs := min(pow2(attempt), 60.0)
			backoff := time.Duration(backoffSecs * float64(time.Second))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		chunkFile, currentSize, err := OpenChunkFile(tmpPath, partPath)
		if err != nil {
			if err.Error() == "chunk already complete" {
				return nil
			}
			return err
		}

		// Seed the progress display from the resume offset
		if currentSize > 0 {
			d.progress.SeedChunk(index, currentSize)
		}

		resumeStart := start + currentSize
		if resumeStart > end {
			resumeStart = end + 1
		}

		if resumeStart <= end {
			progressWriter := &progressWriter{
				writer:   chunkFile,
				tracker:  d.progress,
				chunkIdx: index,
			}

			err = d.client.DownloadRange(ctx, d.args.URL, resumeStart, end, progressWriter)
			if err != nil {
				chunkFile.Close()
				lastErr = err

				if ctx.Err() != nil {
					return ctx.Err()
				}

				continue
			}
		}

		if err := chunkFile.Finalize(); err != nil {
			return fmt.Errorf("failed to finalize chunk: %w", err)
		}

		return nil
	}

	d.progress.PrintError(index, lastErr)
	return fmt.Errorf("download failed after %d retries: %w", maxRetries, lastErr)
}

// pow2 returns 2^n as a float64
func pow2(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 2.0
	}
	return result
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// GetArguments returns the current download arguments.
func (d *Downloader) GetArguments() *DownloadArguments {
	return d.args
}

// extractFilenameFromURL extracts a filename from a URL
func extractFilenameFromURL(url string) string {
	base := url
	for _, sep := range []string{"?", "#"} {
		if idx := len(base) - 1; idx >= 0 {
			for i := len(base) - 1; i >= 0; i-- {
				if base[i] == sep[0] {
					base = base[:i]
					break
				}
			}
		}
	}

	return filepath.Base(base)
}

// progressWriter wraps a writer to track progress
type progressWriter struct {
	writer   *ChunkFile
	tracker  *ProgressTracker
	chunkIdx int
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	if n > 0 {
		pw.tracker.AddBytes(pw.chunkIdx, int64(n))
		pw.tracker.PrintProgress(pw.chunkIdx)
	}
	return
}

// startPostPartWorkers launches worker pool for post-part commands
func (d *Downloader) startPostPartWorkers() {
	numWorkers := d.config.PostPartConcurrency
	if numWorkers == 0 {
		numWorkers = 10
	}

	for i := 0; i < numWorkers; i++ {
		d.postPartWg.Add(1)
		go d.postPartWorker()
	}
}

// postPartWorker processes post-part commands from the channel
func (d *Downloader) postPartWorker() {
	defer d.postPartWg.Done()

	for index := range d.postPartCh {
		partPath := d.args.PartPath(index)

		cmd := d.config.PostPartCmd
		cmd = strings.ReplaceAll(cmd, "{part}", partPath)
		cmd = strings.ReplaceAll(cmd, "{idx}", strconv.Itoa(index))
		cmd = strings.ReplaceAll(cmd, "{base}", d.args.FilenamePrefix)

		d.progress.PrintCmdMessage("[post-part chunk %d] Running: %s", index, cmd)

		execCmd := exec.Command("sh", "-c", cmd)
		output, err := execCmd.CombinedOutput()

		if len(output) > 0 {
			indented := "  " + strings.ReplaceAll(strings.TrimSpace(string(output)), "\n", "\n  ")
			d.progress.PrintCmdMessage("[post-part chunk %d] Output:\n%s", index, indented)
		}

		if err != nil {
			d.progress.PrintCmdMessage("[post-part chunk %d] Failed: %v", index, err)
		} else {
			d.progress.PrintCmdMessage("[post-part chunk %d] Completed", index)
		}
	}
}
