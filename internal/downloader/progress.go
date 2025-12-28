package downloader

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressTracker tracks download progress across all chunks
type ProgressTracker struct {
	state         *State
	totalBytes    atomic.Int64
	completedOnce sync.Once
	startTime     time.Time
	isTTY         bool
	mu            sync.Mutex
	lastPrint     time.Time
	writer        io.Writer
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(state *State) *ProgressTracker {
	// Check if stdout is a TTY
	isTTY := isTerminal(os.Stdout)

	return &ProgressTracker{
		state:     state,
		startTime: time.Now(),
		isTTY:     isTTY,
		lastPrint: time.Now(),
		writer:    os.Stdout,
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

// AddBytes increments the total downloaded bytes
func (p *ProgressTracker) AddBytes(n int64) {
	p.totalBytes.Add(n)
}

// PrintProgress prints current progress
func (p *ProgressTracker) PrintProgress(chunkIdx int, chunkBytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Throttle updates to every 500ms when in TTY mode
	now := time.Now()
	if p.isTTY && now.Sub(p.lastPrint) < 500*time.Millisecond {
		return
	}
	p.lastPrint = now

	completed := p.state.GetCompletedCount()
	total := len(p.state.Chunks)
	totalDownloaded := p.totalBytes.Load()
	elapsed := time.Since(p.startTime).Seconds()

	var speed float64
	if elapsed > 0 {
		speed = float64(totalDownloaded) / elapsed
	}

	if p.isTTY {
		// Clear line and print progress
		fmt.Fprintf(p.writer, "\r\033[K[%d/%d] chunk %d: %s/%s @ %s/s",
			completed, total,
			chunkIdx,
			formatBytes(chunkBytes),
			formatBytes(p.state.Chunks[chunkIdx].End-p.state.Chunks[chunkIdx].Start+1),
			formatBytes(int64(speed)))
	} else {
		// Simple progress for non-TTY
		fmt.Fprintf(p.writer, "[%d/%d] chunks completed\n", completed, total)
	}
}

// PrintChunkComplete prints when a chunk completes
func (p *ProgressTracker) PrintChunkComplete(chunkIdx int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	completed := p.state.GetCompletedCount()
	total := len(p.state.Chunks)

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K[%d/%d] chunk %d completed\n",
			completed, total, chunkIdx)
	} else {
		fmt.Fprintf(p.writer, "[%d/%d] chunk %d completed\n",
			completed, total, chunkIdx)
	}
}

// PrintComplete prints final completion message
func (p *ProgressTracker) PrintComplete() {
	p.completedOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		elapsed := time.Since(p.startTime)
		totalSize := p.state.TotalSize
		avgSpeed := float64(totalSize) / elapsed.Seconds()

		if p.isTTY {
			fmt.Fprintf(p.writer, "\r\033[K")
		}

		fmt.Fprintf(p.writer, "Download complete: %s in %s (avg %s/s)\n",
			formatBytes(totalSize),
			formatDuration(elapsed),
			formatBytes(int64(avgSpeed)))
	})
}

// PrintError prints an error message
func (p *ProgressTracker) PrintError(chunkIdx int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K")
	}
	fmt.Fprintf(p.writer, "chunk %d error: %v\n", chunkIdx, err)
}

// PrintMessage prints a message, properly handling TTY line clearing
func (p *ProgressTracker) PrintMessage(format string, args ...interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY {
		fmt.Fprintf(p.writer, "\r\033[K")
	}
	fmt.Fprintf(p.writer, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprintln(p.writer)
	}
}

// PrintCmdMessage prints command-related messages with visual distinction
func (p *ProgressTracker) PrintCmdMessage(format string, args ...interface{}) {
	dimStart := ""
	dimEnd := ""
	if p.isTTY {
		dimStart = "\033[2m" // Dim/grey
		dimEnd = "\033[0m"   // Reset
	}

	msg := fmt.Sprintf(format, args...)
	p.PrintMessage("%s→ %s%s", dimStart, msg, dimEnd)
}

// IsTTY returns whether output is to a TTY
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
