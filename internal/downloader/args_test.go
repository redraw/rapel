package downloader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPartPath(t *testing.T) {
	args := &DownloadArguments{FilenamePrefix: "test-file"}

	tests := []struct {
		name     string
		index    int
		expected string
	}{
		{name: "first chunk", index: 0, expected: "test-file.000000.part"},
		{name: "second chunk", index: 1, expected: "test-file.000001.part"},
		{name: "large index", index: 123456, expected: "test-file.123456.part"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, args.PartPath(tt.index))
		})
	}
}

func TestTmpPath(t *testing.T) {
	args := &DownloadArguments{FilenamePrefix: "test-file"}

	tests := []struct {
		name     string
		index    int
		expected string
	}{
		{name: "first chunk", index: 0, expected: "test-file.000000.tmp"},
		{name: "second chunk", index: 1, expected: "test-file.000001.tmp"},
		{name: "large index", index: 123456, expected: "test-file.123456.tmp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, args.TmpPath(tt.index))
		})
	}
}

func TestNewDownloadArguments(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		totalSize      int64
		chunkSize      int64
		prefix         string
		expectedChunks int
	}{
		{name: "single chunk", totalSize: 1000, chunkSize: 1000, expectedChunks: 1},
		{name: "multiple chunks", totalSize: 2500, chunkSize: 1000, expectedChunks: 3},
		{name: "exact multiple", totalSize: 3000, chunkSize: 1000, expectedChunks: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := NewDownloadArguments(tt.url, tt.totalSize, tt.chunkSize, tt.prefix)

			assert.Equal(t, tt.url, args.URL)
			assert.Equal(t, tt.totalSize, args.TotalSize)
			assert.Equal(t, tt.chunkSize, args.ChunkSize)
			assert.Equal(t, tt.prefix, args.FilenamePrefix)
			assert.Equal(t, tt.expectedChunks, args.NumChunks())
		})
	}
}

func TestChunkRange(t *testing.T) {
	args := NewDownloadArguments("http://example.com/file", 2500, 1000, "test")

	tests := []struct {
		index         int
		expectedStart int64
		expectedEnd   int64
	}{
		{index: 0, expectedStart: 0, expectedEnd: 999},
		{index: 1, expectedStart: 1000, expectedEnd: 1999},
		{index: 2, expectedStart: 2000, expectedEnd: 2499}, // last chunk, capped at totalSize-1
	}

	for _, tt := range tests {
		start, end := args.ChunkRange(tt.index)
		assert.Equal(t, tt.expectedStart, start, "chunk %d start", tt.index)
		assert.Equal(t, tt.expectedEnd, end, "chunk %d end", tt.index)
	}
}

func TestChunkSizeAt(t *testing.T) {
	args := NewDownloadArguments("http://example.com/file", 2500, 1000, "test")

	assert.Equal(t, int64(1000), args.ChunkSizeAt(0))
	assert.Equal(t, int64(1000), args.ChunkSizeAt(1))
	assert.Equal(t, int64(500), args.ChunkSizeAt(2)) // last chunk is smaller
}
