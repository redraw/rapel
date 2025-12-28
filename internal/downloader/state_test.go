package downloader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetChunkFilename(t *testing.T) {
	state := &State{FilenamePrefix: "test-file"}

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
			assert.Equal(t, tt.expected, state.GetChunkFilename(tt.index))
		})
	}
}

func TestGetChunkTmpFilename(t *testing.T) {
	state := &State{FilenamePrefix: "test-file"}

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
			assert.Equal(t, tt.expected, state.GetChunkTmpFilename(tt.index))
		})
	}
}

func TestNewState(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		totalSize      int64
		chunkSize      int64
		prefix         string
		expectedChunks int
	}{
		{name: "single chunk", url: "http://example.com/file", totalSize: 1000, chunkSize: 1000, prefix: "test", expectedChunks: 1},
		{name: "multiple chunks", url: "http://example.com/file", totalSize: 2500, chunkSize: 1000, prefix: "test", expectedChunks: 3},
		{name: "exact multiple", url: "http://example.com/file", totalSize: 3000, chunkSize: 1000, prefix: "test", expectedChunks: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewState(tt.url, tt.totalSize, tt.chunkSize, tt.prefix)

			assert.Equal(t, tt.url, state.URL)
			assert.Equal(t, tt.totalSize, state.TotalSize)
			assert.Equal(t, tt.chunkSize, state.ChunkSize)
			assert.Equal(t, tt.prefix, state.FilenamePrefix)
			assert.Len(t, state.Chunks, tt.expectedChunks)

			// Verify chunk boundaries
			for i, chunk := range state.Chunks {
				expectedStart := int64(i) * tt.chunkSize
				expectedEnd := expectedStart + tt.chunkSize - 1
				if expectedEnd >= tt.totalSize {
					expectedEnd = tt.totalSize - 1
				}

				assert.Equal(t, expectedStart, chunk.Start)
				assert.Equal(t, expectedEnd, chunk.End)
				assert.False(t, chunk.Completed)
			}
		})
	}
}

func TestMarkChunkCompleted(t *testing.T) {
	state := NewState("http://example.com/file", 3000, 1000, "test")

	assert.Equal(t, 0, state.CompletedCount)

	// Mark first chunk as completed
	state.MarkChunkCompleted(0, 1000)
	assert.Equal(t, 1, state.CompletedCount)
	assert.True(t, state.Chunks[0].Completed)
	assert.Equal(t, int64(1000), state.Chunks[0].Downloaded)

	// Mark same chunk again (should not increment)
	state.MarkChunkCompleted(0, 1000)
	assert.Equal(t, 1, state.CompletedCount)

	// Mark second chunk
	state.MarkChunkCompleted(1, 1000)
	assert.Equal(t, 2, state.CompletedCount)
}
