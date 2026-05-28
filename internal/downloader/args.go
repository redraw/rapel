package downloader

import (
	"encoding/json"
	"fmt"
	"os"
)

// DownloadArguments holds the arguments that determine chunk layout and file
// identity (URL, total size, chunk size, filename prefix). These are persisted
// to .{prefix}-args.json at the start of a download so that a subsequent run
// can refuse to resume against incompatible inputs. Runtime knobs like
// --jobs, --post-part, proxy, retries, etc. are intentionally NOT persisted —
// they can change freely between runs without invalidating chunks on disk.
type DownloadArguments struct {
	URL            string `json:"url"`
	TotalSize      int64  `json:"total_size"`
	ChunkSize      int64  `json:"chunk_size"`
	FilenamePrefix string `json:"filename_prefix"`

	filePath string // unexported, set after New/Load
}

// NewDownloadArguments creates a new DownloadArguments.
func NewDownloadArguments(url string, totalSize, chunkSize int64, prefix string) *DownloadArguments {
	return &DownloadArguments{
		URL:            url,
		TotalSize:      totalSize,
		ChunkSize:      chunkSize,
		FilenamePrefix: prefix,
		filePath:       fmt.Sprintf(".%s-args.json", prefix),
	}
}

// LoadDownloadArguments loads args from a JSON file, or returns (nil, nil) if not found.
func LoadDownloadArguments(prefix string) (*DownloadArguments, error) {
	filePath := fmt.Sprintf(".%s-args.json", prefix)

	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read args file: %w", err)
	}

	var args DownloadArguments
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, fmt.Errorf("failed to parse args file: %w", err)
	}

	args.filePath = filePath
	return &args, nil
}

// Save writes args to a JSON file atomically. Should be called once at the start of a download.
func (a *DownloadArguments) Save() error {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal args: %w", err)
	}

	tmpPath := a.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write args file: %w", err)
	}

	if err := os.Rename(tmpPath, a.filePath); err != nil {
		return fmt.Errorf("failed to rename args file: %w", err)
	}

	return nil
}

// Delete removes the args file.
func (a *DownloadArguments) Delete() error {
	return os.Remove(a.filePath)
}

// NumChunks returns the total number of chunks for this download.
func (a *DownloadArguments) NumChunks() int {
	return int((a.TotalSize + a.ChunkSize - 1) / a.ChunkSize)
}

// ChunkRange returns the byte range [start, end] (inclusive) for chunk i.
func (a *DownloadArguments) ChunkRange(i int) (start, end int64) {
	start = int64(i) * a.ChunkSize
	end = start + a.ChunkSize - 1
	if end >= a.TotalSize {
		end = a.TotalSize - 1
	}
	return
}

// ChunkSizeAt returns the number of bytes in chunk i.
func (a *DownloadArguments) ChunkSizeAt(i int) int64 {
	start, end := a.ChunkRange(i)
	return end - start + 1
}

// PartPath returns the .part filename for chunk i.
func (a *DownloadArguments) PartPath(i int) string {
	return fmt.Sprintf("%s.%06d.part", a.FilenamePrefix, i)
}

// TmpPath returns the .tmp filename for chunk i.
func (a *DownloadArguments) TmpPath(i int) string {
	return fmt.Sprintf("%s.%06d.tmp", a.FilenamePrefix, i)
}
