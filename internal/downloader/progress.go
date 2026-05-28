package downloader

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressTracker tracks download progress across all chunks.
// It is the sole owner of runtime progress state. Per-chunk byte counters are
// atomic; the only mutex guards stdout serialization and throttling.
// Single-writer-per-chunk is assumed: only one goroutine downloads a given chunk.
type ProgressTracker struct {
	// immutable after construction
	numChunks  int
	chunkSizes []int64
	totalSize  int64
	startTime  time.Time
	isTTY      bool
	writer     io.Writer

	// per-chunk progress (atomic; single-writer-per-chunk invariant)
	chunkProgress []atomic.Int64 // bytes for this chunk (seeded offset + bytes added this session)
	totalBytes    atomic.Int64   // only bytes added via AddBytes (for speed calculation)
	chunkDone     []atomic.Bool  // set only by MarkComplete; IsChunkComplete reads this
	chunkOnce     []sync.Once    // ensures completed count increments exactly once per chunk
	completed     atomic.Int32

	// stdout serialization and throttle only
	printMu       sync.Mutex
	lastPrint     time.Time
	completedOnce sync.Once
}

// NewProgressTracker creates a tracker from download arguments.
func NewProgressTracker(args *DownloadArguments) *ProgressTracker {
	n := args.NumChunks()
	sizes := make([]int64, n)
	for i := range sizes {
		sizes[i] = args.ChunkSizeAt(i)
	}

	return &ProgressTracker{
		numChunks:     n,
		chunkSizes:    sizes,
		totalSize:     args.TotalSize,
		startTime:     time.Now(),
		isTTY:         isTerminal(os.Stdout),
		lastPrint:     time.Now(),
		writer:        os.Stdout,
		chunkProgress: make([]atomic.Int64, n),
		chunkDone:     make([]atomic.Bool, n),
		chunkOnce:     make([]sync.Once, n),
	}
}

// isTerminal checks if the file is a terminal
func isTerminal(f *os.File) bool {
	fileInfo, err := f.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// SeedChunk sets the initial chunkProgress for a resumed chunk (e.g., from .tmp file size).
// Does NOT mark the chunk complete; the download worker must still finalize it.
// Called by the download goroutine before writing begins. Does not update totalBytes.
func (p *ProgressTracker) SeedChunk(chunkIdx int, bytes int64) {
	p.chunkProgress[chunkIdx].Store(bytes)
}

// MarkComplete marks a chunk as fully done and bumps the completed counter exactly once.
// Called for .part files found at startup (resume) and after a successful Finalize().
func (p *ProgressTracker) MarkComplete(chunkIdx int) {
	p.chunkProgress[chunkIdx].Store(p.chunkSizes[chunkIdx])
	p.chunkDone[chunkIdx].Store(true)
	p.chunkOnce[chunkIdx].Do(func() {
		p.completed.Add(1)
	})
}

// AddBytes records n newly-downloaded bytes for chunkIdx.
// Must be called from a single goroutine per chunk.
func (p *ProgressTracker) AddBytes(chunkIdx int, n int64) {
	p.chunkProgress[chunkIdx].Add(n)
	p.totalBytes.Add(n)
}

// IsChunkComplete returns true when MarkComplete has been called for this chunk.
func (p *ProgressTracker) IsChunkComplete(chunkIdx int) bool {
	return p.chunkDone[chunkIdx].Load()
}

// Bytes returns the bytes currently recorded for chunkIdx.
func (p *ProgressTracker) Bytes(chunkIdx int) int64 {
	return p.chunkProgress[chunkIdx].Load()
}

// ExpectedSize returns the expected total bytes for chunkIdx.
func (p *ProgressTracker) ExpectedSize(chunkIdx int) int64 {
	return p.chunkSizes[chunkIdx]
}

// CompletedCount returns the number of fully completed chunks.
func (p *ProgressTracker) CompletedCount() int {
	return int(p.completed.Load())
}

// NumChunks returns the total number of chunks.
func (p *ProgressTracker) NumChunks() int {
	return p.numChunks
}

// TotalBytes returns bytes downloaded this session (excludes seeded resume bytes).
func (p *ProgressTracker) TotalBytes() int64 {
	return p.totalBytes.Load()
}

// PrintProgress prints current progress for the given chunk.
func (p *ProgressTracker) PrintProgress(chunkIdx int) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	now := time.Now()
	if p.isTTY && now.Sub(p.lastPrint) < 500*time.Millisecond {
		return
	}
	p.lastPrint = now

	completed := int(p.completed.Load())
	totalDownloaded := p.totalBytes.Load()
	elapsed := time.Since(p.startTime).Seconds()

	var speed float64
	if elapsed > 0 {
		speed = float64(totalDownloaded) / elapsed
	}

	chunkBytes := p.chunkProgress[chunkIdx].Load()

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K[%d/%d] chunk %d: %s/%s @ %s/s",
			completed, p.numChunks,
			chunkIdx,
			formatBytes(chunkBytes),
			formatBytes(p.chunkSizes[chunkIdx]),
			formatBytes(int64(speed)))
	} else {
		fmt.Fprintf(p.writer, "[%d/%d] chunks completed\n", completed, p.numChunks)
	}
}

// PrintChunkComplete prints when a chunk completes.
func (p *ProgressTracker) PrintChunkComplete(chunkIdx int) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	completed := int(p.completed.Load())

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K[%d/%d] chunk %d completed\n",
			completed, p.numChunks, chunkIdx)
	} else {
		fmt.Fprintf(p.writer, "[%d/%d] chunk %d completed\n",
			completed, p.numChunks, chunkIdx)
	}
}

// PrintComplete prints the final completion message.
func (p *ProgressTracker) PrintComplete() {
	p.completedOnce.Do(func() {
		p.printMu.Lock()
		defer p.printMu.Unlock()

		elapsed := time.Since(p.startTime)
		avgSpeed := float64(p.totalSize) / elapsed.Seconds()

		if p.isTTY {
			fmt.Fprintf(p.writer, "\r\033[K")
		}

		fmt.Fprintf(p.writer, "Download complete: %s in %s (avg %s/s)\n",
			formatBytes(p.totalSize),
			formatDuration(elapsed),
			formatBytes(int64(avgSpeed)))
	})
}

// PrintError prints an error message.
func (p *ProgressTracker) PrintError(chunkIdx int, err error) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K")
	}
	fmt.Fprintf(p.writer, "chunk %d error: %v\n", chunkIdx, err)
}

// PrintMessage prints a message, properly handling TTY line clearing.
func (p *ProgressTracker) PrintMessage(format string, args ...interface{}) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K")
	}
	fmt.Fprintf(p.writer, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprintln(p.writer)
	}
}

// PrintCmdMessage prints command-related messages with visual distinction.
func (p *ProgressTracker) PrintCmdMessage(format string, args ...interface{}) {
	dimStart := ""
	dimEnd := ""
	if p.isTTY {
		dimStart = "\033[2m"
		dimEnd = "\033[0m"
	}

	msg := fmt.Sprintf(format, args...)
	p.PrintMessage("%s→ %s%s", dimStart, msg, dimEnd)
}

// IsTTY returns whether output is to a TTY.
func (p *ProgressTracker) IsTTY() bool {
	return p.isTTY
}

// formatBytes formats bytes in human-readable format
func formatBytes(bytes int64) string {
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// formatDuration formats duration in human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
