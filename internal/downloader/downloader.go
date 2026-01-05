package downloader

import (
	"context"
	"fmt"
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
	state      *State
	client     *httpclient.Client
	progress   *ProgressTracker
	postPartWg sync.WaitGroup
	postPartCh chan int // Channel for post-part worker pool
}

// NewDownloader creates a new Downloader
func NewDownloader(config Config) (*Downloader, error) {
	client, err := httpclient.NewClient(config.HTTPConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	d := &Downloader{
		config: config,
		client: client,
	}

	return d, nil
}

// Download performs the chunked download
func (d *Downloader) Download(ctx context.Context) error {
	// Extract filename prefix from URL
	prefix := extractFilenameFromURL(d.config.URL)
	if prefix == "" {
		prefix = "download"
	}

	// Check for existing state
	var err error
	if !d.config.Force {
		d.state, err = LoadState(prefix)
		if err != nil {
			return fmt.Errorf("failed to load state: %w", err)
		}
	}

	// Get content length if not provided
	totalSize := d.config.TotalSize
	if totalSize == 0 {
		totalSize, err = d.client.GetContentLength(ctx, d.config.URL)
		if err != nil {
			return fmt.Errorf("failed to get content length: %w", err)
		}
	}

	// Create new state if needed
	if d.state == nil {
		d.state = NewState(d.config.URL, totalSize, d.config.ChunkSize, prefix)
	}

	// Validate state matches current config
	if d.state.URL != d.config.URL || d.state.TotalSize != totalSize {
		if !d.config.Force {
			return fmt.Errorf("existing state doesn't match URL/size, use --force to restart")
		}
		d.state = NewState(d.config.URL, totalSize, d.config.ChunkSize, prefix)
	}

	// Create progress tracker
	d.progress = NewProgressTracker(d.state)

	// Print download info
	fmt.Printf("URL        : %s\n", d.config.URL)
	fmt.Printf("File       : %s\n", prefix)
	fmt.Printf("Size       : %s\n", formatBytes(totalSize))
	fmt.Printf("Chunk size : %s\n", formatBytes(d.config.ChunkSize))
	fmt.Printf("Chunks     : %d\n", len(d.state.Chunks))
	fmt.Printf("Jobs       : %d\n", d.config.MaxConcurrency)
	fmt.Println()

	// Download all chunks
	if err := d.downloadAllChunks(ctx); err != nil {
		return err
	}

	d.progress.PrintComplete()

	// Delete state file after successful completion
	if err := d.state.Delete(); err != nil {
		return fmt.Errorf("failed to delete state file: %w", err)
	}

	return nil
}

// downloadAllChunks downloads all chunks using a worker pool with sequential dispatch
func (d *Downloader) downloadAllChunks(ctx context.Context) error {
	// Create context that can be cancelled on first error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create work channel and error channel
	type workItem struct {
		index         int
		chunk         *ChunkInfo
		needsDownload bool
		needsPostPart bool
	}
	workChan := make(chan workItem)
	errChan := make(chan error, 1)

	// Start post-part workers if post-part command is configured
	if d.config.HasPostPartCmd() {
		// Use buffered channel to avoid blocking download workers
		d.postPartCh = make(chan int, len(d.state.Chunks))
		d.startPostPartWorkers()
	}

	// Launch worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < d.config.MaxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				// Download chunk if needed
				if work.needsDownload {
					if err := d.downloadChunk(ctx, work.index, work.chunk); err != nil {
						select {
						case errChan <- fmt.Errorf("chunk %d: %w", work.index, err):
							cancel() // Cancel other downloads on error
						default:
						}
						return
					}

					// Mark as completed and save state
					d.state.MarkChunkCompleted(work.index, work.chunk.End-work.chunk.Start+1)
					d.progress.PrintChunkComplete(work.index)

					if err := d.state.Save(); err != nil {
						select {
						case errChan <- fmt.Errorf("failed to save state: %w", err):
						default:
						}
						return
					}
				} else if work.needsPostPart {
					// Chunk is complete but post-part needs retry
					d.progress.PrintMessage("Retrying post-part for chunk %d", work.index)
				}

				// Send to post-part worker pool if configured
				if d.config.HasPostPartCmd() {
					select {
					case d.postPartCh <- work.index:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Dispatch chunks in sequential order
	go func() {
		defer close(workChan)
		for i, chunk := range d.state.Chunks {
			// Check if we need to process this chunk
			needsDownload := !chunk.Completed
			needsPostPart := chunk.Completed && d.config.HasPostPartCmd() && !chunk.PostPartCompleted

			// Skip if already complete and no post-part work needed
			if !needsDownload && !needsPostPart {
				continue
			}

			select {
			case workChan <- workItem{
				index:         i,
				chunk:         chunk,
				needsDownload: needsDownload,
				needsPostPart: needsPostPart,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for all workers to finish
	wg.Wait()
	close(errChan)

	// Close post-part channel and wait for workers
	if d.config.HasPostPartCmd() {
		close(d.postPartCh)
		d.progress.PrintMessage("Waiting for post-part commands to complete...")
		d.postPartWg.Wait()
	}

	// Return first error if any
	if err := <-errChan; err != nil {
		return err
	}

	return nil
}

// downloadChunk downloads a single chunk with resume support and retry logic
func (d *Downloader) downloadChunk(ctx context.Context, index int, chunk *ChunkInfo) error {
	tmpPath := d.state.GetChunkTmpFilename(index)
	partPath := d.state.GetChunkFilename(index)

	var lastErr error
	maxRetries := d.config.HTTPConfig.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Exponential backoff on retry
		if attempt > 0 {
			backoffSecs := min(pow2(attempt), 60.0)
			backoff := time.Duration(backoffSecs * float64(time.Second))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Open/reopen chunk file (will resume if .tmp exists)
		chunkFile, currentSize, err := OpenChunkFile(tmpPath, partPath)
		if err != nil {
			if err.Error() == "chunk already complete" {
				return nil
			}
			return err
		}

		// Calculate resume position
		resumeStart := chunk.Start + currentSize
		if resumeStart > chunk.End {
			resumeStart = chunk.End + 1
		}

		// Download remaining bytes
		if resumeStart <= chunk.End {
			// Create progress writer that updates progress tracker
			progressWriter := &progressWriter{
				writer:   chunkFile,
				tracker:  d.progress,
				chunkIdx: index,
				current:  currentSize,
			}

			err = d.client.DownloadRange(ctx, d.state.URL, resumeStart, chunk.End, progressWriter)
			if err != nil {
				chunkFile.Close()
				lastErr = err

				// Don't retry on context cancellation
				if ctx.Err() != nil {
					return ctx.Err()
				}

				// Retry
				continue
			}
		}

		// Finalize chunk (rename .tmp to .part)
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

// GetState returns the current state
func (d *Downloader) GetState() *State {
	return d.state
}

// extractFilenameFromURL extracts a filename from a URL
func extractFilenameFromURL(url string) string {
	// Remove query string and fragment
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

	// Extract filename from path
	return filepath.Base(base)
}

// progressWriter wraps a writer to track progress
type progressWriter struct {
	writer   *ChunkFile
	tracker  *ProgressTracker
	chunkIdx int
	current  int64
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	if n > 0 {
		pw.current += int64(n)
		pw.tracker.AddBytes(int64(n))
		pw.tracker.PrintProgress(pw.chunkIdx, pw.current)
	}
	return
}

// startPostPartWorkers launches worker pool for post-part commands
func (d *Downloader) startPostPartWorkers() {
	// Determine number of workers (default to unlimited if 0)
	numWorkers := d.config.PostPartConcurrency
	if numWorkers == 0 {
		numWorkers = 10 // Reasonable default for unlimited
	}

	// Launch workers
	for i := 0; i < numWorkers; i++ {
		d.postPartWg.Add(1)
		go d.postPartWorker()
	}
}

// postPartWorker processes post-part commands from the channel
func (d *Downloader) postPartWorker() {
	defer d.postPartWg.Done()

	for index := range d.postPartCh {
		partPath := d.state.GetChunkFilename(index)

		// Substitute placeholders
		cmd := d.config.PostPartCmd
		cmd = strings.ReplaceAll(cmd, "{part}", partPath)
		cmd = strings.ReplaceAll(cmd, "{idx}", strconv.Itoa(index))
		cmd = strings.ReplaceAll(cmd, "{base}", d.state.FilenamePrefix)

		d.progress.PrintCmdMessage("[post-part chunk %d] Running: %s", index, cmd)

		// Execute command using shell and capture output
		execCmd := exec.Command("sh", "-c", cmd)
		output, err := execCmd.CombinedOutput()

		if len(output) > 0 {
			// Indent each line of output
			indented := "  " + strings.ReplaceAll(strings.TrimSpace(string(output)), "\n", "\n  ")
			d.progress.PrintCmdMessage("[post-part chunk %d] Output:\n%s", index, indented)
		}

		if err != nil {
			d.progress.PrintCmdMessage("[post-part chunk %d] Failed: %v", index, err)
			d.state.MarkPostPartCompleted(index, false)
		} else {
			d.progress.PrintCmdMessage("[post-part chunk %d] Completed", index)
			d.state.MarkPostPartCompleted(index, true)
		}

		// Save state after post-part completion
		if err := d.state.Save(); err != nil {
			d.progress.PrintCmdMessage("[post-part chunk %d] Warning: failed to save state: %v", index, err)
		}
	}
}
