package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ChunkInfo represents the state of a single chunk
type ChunkInfo struct {
	Index             int   `json:"index"`
	Start             int64 `json:"start"`
	End               int64 `json:"end"`
	Downloaded        int64 `json:"downloaded"`
	Completed         bool  `json:"completed"`
	PostPartCompleted bool  `json:"post_part_completed"`
}

// State represents the overall download state
type State struct {
	URL            string       `json:"url"`
	TotalSize      int64        `json:"total_size"`
	ChunkSize      int64        `json:"chunk_size"`
	FilenamePrefix string       `json:"filename_prefix"`
	Chunks         []*ChunkInfo `json:"chunks"`
	CompletedCount int          `json:"completed_count"`

	mu       sync.Mutex
	filePath string
}

// NewState creates a new State
func NewState(url string, totalSize, chunkSize int64, prefix string) *State {
	numChunks := (totalSize + chunkSize - 1) / chunkSize
	chunks := make([]*ChunkInfo, numChunks)

	for i := int64(0); i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}

		chunks[i] = &ChunkInfo{
			Index:      int(i),
			Start:      start,
			End:        end,
			Downloaded: 0,
			Completed:  false,
		}
	}

	return &State{
		URL:            url,
		TotalSize:      totalSize,
		ChunkSize:      chunkSize,
		FilenamePrefix: prefix,
		Chunks:         chunks,
		CompletedCount: 0,
		filePath:       fmt.Sprintf(".%s-state.json", prefix),
	}
}

// LoadState loads state from a JSON file, or returns nil if not found
func LoadState(prefix string) (*State, error) {
	filePath := fmt.Sprintf(".%s-state.json", prefix)

	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	state.filePath = filePath
	return &state, nil
}

// Save saves the state to a JSON file
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// MarkChunkCompleted marks a chunk as completed and updates the counter
func (s *State) MarkChunkCompleted(index int, downloaded int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.Chunks) {
		return
	}

	chunk := s.Chunks[index]
	if !chunk.Completed {
		chunk.Completed = true
		chunk.Downloaded = downloaded
		s.CompletedCount++
	}
}

// MarkPostPartCompleted marks a chunk's post-part hook as completed
func (s *State) MarkPostPartCompleted(index int, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.Chunks) {
		return
	}

	s.Chunks[index].PostPartCompleted = success
}

// UpdateChunkProgress updates the downloaded bytes for a chunk
func (s *State) UpdateChunkProgress(index int, downloaded int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.Chunks) {
		return
	}

	s.Chunks[index].Downloaded = downloaded
}

// GetChunkFilename returns the .part filename for a chunk
func (s *State) GetChunkFilename(index int) string {
	return fmt.Sprintf("%s.%06d.part", s.FilenamePrefix, index)
}

// GetChunkTmpFilename returns the .tmp filename for a chunk
func (s *State) GetChunkTmpFilename(index int) string {
	return fmt.Sprintf("%s.%06d.tmp", s.FilenamePrefix, index)
}

// GetCompletedCount returns the number of completed chunks (thread-safe)
func (s *State) GetCompletedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CompletedCount
}

// CleanupTempFiles removes all temporary files for this download
func (s *State) CleanupTempFiles() error {
	pattern := fmt.Sprintf("%s.*.tmp", s.FilenamePrefix)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, file := range matches {
		os.Remove(file)
	}

	return nil
}

// Delete removes the state file
func (s *State) Delete() error {
	return os.Remove(s.filePath)
}
