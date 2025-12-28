package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redraw/rapel/internal/downloader"
	httpclient "github.com/redraw/rapel/internal/http"
	"github.com/redraw/rapel/internal/merger"
)

// DownloadCommand implements the download subcommand
func DownloadCommand(args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)

	// Define flags
	chunkSizeStr := fs.String("c", "100M", "Chunk size (e.g., 50M, 1G)")
	proxyURL := fs.String("x", "", "Proxy URL (e.g., socks5h://127.0.0.1:9050)")
	retries := fs.Int("r", 10, "Retries per request")
	noHead := fs.Bool("no-head", false, "Skip HEAD request (requires --size)")
	sizeStr := fs.String("size", "", "Total size in bytes (required if --no-head)")
	jobs := fs.Int("jobs", 1, "Concurrent chunks")
	force := fs.Bool("force", false, "Force re-download even if state exists")
	merge := fs.Bool("merge", false, "Merge chunks after download (auto-detects output name)")
	postPart := fs.String("post-part", "", "Command to run after each part completes (supports {part}, {idx}, {base})")
	postPartJobs := fs.Int("post-part-jobs", 0, "Max concurrent post-part commands (0 = unlimited)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: rapel download [options] URL

Download a file using chunked HTTP Range requests with resume support.

IMPORTANT: Flags must be specified BEFORE the URL.

Options:
  -c SIZE            Chunk size (K, M, G suffix). Default: 100M
  -x URL             Proxy URL (e.g., socks5h://127.0.0.1:9050)
  -r N               Retries per request. Default: 10
  --no-head          Skip HEAD request (requires --size)
  --size BYTES       Total size in bytes (required if --no-head)
  --jobs N           Concurrent chunks. Default: 1
  --force            Force re-download even if state exists
  --merge            Merge chunks after download (auto-detects output name)
  --post-part CMD    Command to run after each part completes
                     Placeholders: {part} {idx} {base}
  --post-part-jobs N Max concurrent post-part commands. Default: 0 (unlimited)

Examples:
  rapel download https://example.com/file.bin
  rapel download -c 50M --jobs 4 https://example.com/file.bin
  rapel download -x socks5h://127.0.0.1:9050 https://example.com/file.bin
  rapel download --merge https://example.com/file.bin
  rapel download --post-part 'rclone move {part} r2:bucket/' https://example.com/file.bin
`)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("URL is required")
	}

	url := fs.Arg(0)

	// Parse chunk size
	chunkSize, err := parseSize(*chunkSizeStr)
	if err != nil {
		return fmt.Errorf("invalid chunk size: %w", err)
	}

	// Parse total size if provided
	var totalSize int64
	if *sizeStr != "" {
		totalSize, err = strconv.ParseInt(*sizeStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid size: %w", err)
		}
	}

	// Validate --no-head requires --size
	if *noHead && totalSize == 0 {
		return fmt.Errorf("--no-head requires --size")
	}

	// Create downloader config
	config := downloader.Config{
		URL:                 url,
		ChunkSize:           chunkSize,
		MaxConcurrency:      *jobs,
		Force:               *force,
		TotalSize:           totalSize,
		PostPartCmd:         *postPart,
		PostPartConcurrency: *postPartJobs,
		HTTPConfig: httpclient.Config{
			ProxyURL:       *proxyURL,
			MaxRetries:     *retries,
			ConnectTimeout: 30 * time.Second,
			ReadTimeout:    60 * time.Second,
		},
	}

	// Create downloader
	dl, err := downloader.NewDownloader(config)
	if err != nil {
		return fmt.Errorf("failed to create downloader: %w", err)
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	// Perform download
	if err := dl.Download(ctx); err != nil {
		if err == context.Canceled {
			fmt.Println("Download cancelled")
			// Save state before exit
			if state := dl.GetState(); state != nil {
				state.Save()
			}
			return nil
		}
		return err
	}

	// Merge if requested
	if *merge {
		fmt.Println("\nMerging chunks...")

		state := dl.GetState()
		pattern := fmt.Sprintf("%s.*.part", state.FilenamePrefix)

		m := merger.NewMerger(merger.Config{
			Output:  "", // Auto-detect output name
			Pattern: pattern,
			Delete:  false,
		})

		if err := m.Merge(); err != nil {
			return fmt.Errorf("failed to merge: %w", err)
		}
	}

	return nil
}

// parseSize parses a size string with K, M, G suffix
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	multiplier := int64(1)
	suffix := s[len(s)-1]

	switch suffix {
	case 'K', 'k':
		multiplier = 1000
		s = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1000 * 1000
		s = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1000 * 1000 * 1000
		s = s[:len(s)-1]
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}

	return value * multiplier, nil
}
