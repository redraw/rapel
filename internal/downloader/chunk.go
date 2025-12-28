package downloader

import (
	"fmt"
	"os"
)

// ChunkFile wraps file operations for a chunk
type ChunkFile struct {
	tmpPath  string
	partPath string
	file     *os.File
}

// OpenChunkFile opens a chunk file for writing, resuming if .tmp exists
func OpenChunkFile(tmpPath, partPath string) (*ChunkFile, int64, error) {
	// Check if .part already exists (chunk is complete)
	if _, err := os.Stat(partPath); err == nil {
		return nil, 0, fmt.Errorf("chunk already complete")
	}

	// Check if .tmp exists for resume
	var currentSize int64
	if info, err := os.Stat(tmpPath); err == nil {
		currentSize = info.Size()
	}

	// Open file in append mode
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open chunk file: %w", err)
	}

	return &ChunkFile{
		tmpPath:  tmpPath,
		partPath: partPath,
		file:     file,
	}, currentSize, nil
}

// Write writes data to the chunk file
func (c *ChunkFile) Write(p []byte) (n int, err error) {
	return c.file.Write(p)
}

// Close closes the file
func (c *ChunkFile) Close() error {
	return c.file.Close()
}

// Finalize closes the file and renames .tmp to .part
func (c *ChunkFile) Finalize() error {
	if err := c.file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	if err := os.Rename(c.tmpPath, c.partPath); err != nil {
		return fmt.Errorf("failed to rename tmp to part: %w", err)
	}

	return nil
}

// Delete removes the temporary file
func (c *ChunkFile) Delete() error {
	c.file.Close()
	return os.Remove(c.tmpPath)
}

// GetCurrentSize returns the current size of the file
func (c *ChunkFile) GetCurrentSize() (int64, error) {
	info, err := c.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
