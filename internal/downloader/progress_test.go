package downloader

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestTracker(totalSize, chunkSize int64) *ProgressTracker {
	args := NewDownloadArguments("http://example.com/file", totalSize, chunkSize, "test")
	return NewProgressTracker(args)
}

func TestMarkComplete(t *testing.T) {
	p := newTestTracker(3000, 1000)

	assert.False(t, p.IsChunkComplete(0))
	assert.Equal(t, 0, p.CompletedCount())

	p.MarkComplete(0)
	assert.True(t, p.IsChunkComplete(0))
	assert.Equal(t, 1, p.CompletedCount())

	// Idempotent: calling again must not double-count
	p.MarkComplete(0)
	assert.Equal(t, 1, p.CompletedCount())
}

func TestAddBytesReachesExpectedSize(t *testing.T) {
	p := newTestTracker(3000, 1000)

	// Add bytes up to expected size for chunk 0
	p.AddBytes(0, 500)
	assert.False(t, p.IsChunkComplete(0))
	assert.Equal(t, int64(500), p.Bytes(0))

	// MarkComplete should still work cleanly
	p.MarkComplete(0)
	assert.True(t, p.IsChunkComplete(0))
	assert.Equal(t, 1, p.CompletedCount())
}

func TestSeedChunkDoesNotMarkComplete(t *testing.T) {
	p := newTestTracker(3000, 1000)

	// Seed with full chunk size — should NOT mark complete
	p.SeedChunk(0, 1000)
	assert.False(t, p.IsChunkComplete(0))
	assert.Equal(t, 0, p.CompletedCount())

	// totalBytes unaffected by SeedChunk
	assert.Equal(t, int64(0), p.TotalBytes())
}

func TestSeedChunkResumedDisplay(t *testing.T) {
	p := newTestTracker(3000, 1000)

	p.SeedChunk(1, 600)
	assert.Equal(t, int64(600), p.Bytes(1))
	assert.False(t, p.IsChunkComplete(1))

	// AddBytes adds on top of the seeded value
	p.AddBytes(1, 400)
	assert.Equal(t, int64(1000), p.Bytes(1))

	// AddBytes updates totalBytes
	assert.Equal(t, int64(400), p.TotalBytes())
}

func TestAddBytesDoesNotUpdateTotalBytesViaSeed(t *testing.T) {
	p := newTestTracker(3000, 1000)

	p.SeedChunk(0, 500)
	assert.Equal(t, int64(0), p.TotalBytes()) // seed doesn't count toward session speed

	p.AddBytes(0, 200)
	assert.Equal(t, int64(200), p.TotalBytes())
}

func TestCompletedCountAcrossChunks(t *testing.T) {
	p := newTestTracker(3000, 1000)

	p.MarkComplete(0)
	p.MarkComplete(1)
	assert.Equal(t, 2, p.CompletedCount())

	p.MarkComplete(2)
	assert.Equal(t, 3, p.CompletedCount())
	assert.Equal(t, 3, p.NumChunks())
}

func TestConcurrentAddBytes(t *testing.T) {
	// Many goroutines each own one chunk; they all write concurrently.
	const numChunks = 16
	const chunkSize = int64(1000)
	const totalSize = numChunks * chunkSize

	p := newTestTracker(totalSize, chunkSize)

	var wg sync.WaitGroup
	for i := 0; i < numChunks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Simulate 10 writes of 100 bytes each per chunk
			for j := 0; j < 10; j++ {
				p.AddBytes(idx, 100)
			}
			p.MarkComplete(idx)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(totalSize), p.TotalBytes())
	assert.Equal(t, numChunks, p.CompletedCount())

	for i := 0; i < numChunks; i++ {
		assert.True(t, p.IsChunkComplete(i), "chunk %d should be complete", i)
	}
}
