package merger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Config holds merger configuration
type Config struct {
	Output  string
	Pattern string
	Delete  bool
}

// Merger handles merging chunk files
type Merger struct {
	config Config
}

// NewMerger creates a new Merger
func NewMerger(config Config) *Merger {
	if config.Pattern == "" {
		config.Pattern = "*.part"
	}
	return &Merger{config: config}
}

// Merge merges all matching chunk files into the output file
func (m *Merger) Merge() error {
	// Find all matching files
	matches, err := filepath.Glob(m.config.Pattern)
	if err != nil {
		return fmt.Errorf("failed to find matching files: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Errorf("no files match pattern: %s", m.config.Pattern)
	}

	// Group files by basename
	basenameGroups := groupFilesByBasename(matches)

	// Determine which files to merge
	if m.config.Output != "" {
		// Output specified: merge single group
		return m.mergeGroup(m.config.Output, basenameGroups)
	}

	// No output specified: auto-detect and merge all groups
	if len(basenameGroups) == 0 {
		return fmt.Errorf("no valid chunk files found")
	}

	if len(basenameGroups) == 1 {
		// Single basename group: use it
		for basename := range basenameGroups {
			fmt.Printf("Auto-detected output name: %s\n", basename)
			return m.mergeGroup(basename, basenameGroups)
		}
	}

	// Multiple basename groups: merge all of them
	var basenames []string
	for basename := range basenameGroups {
		basenames = append(basenames, basename)
	}
	sort.Strings(basenames)

	fmt.Printf("Found %d download sessions to merge:\n", len(basenames))
	for _, basename := range basenames {
		fmt.Printf("  - %s (%d files)\n", basename, len(basenameGroups[basename]))
	}
	fmt.Println()

	// Merge each group
	for _, basename := range basenames {
		if err := m.mergeGroup(basename, basenameGroups); err != nil {
			return fmt.Errorf("failed to merge %s: %w", basename, err)
		}
		fmt.Println()
	}

	return nil
}

// mergeGroup merges a single basename group
func (m *Merger) mergeGroup(outputName string, basenameGroups map[string][]string) error {
	// Find files for this basename
	filesToMerge, found := basenameGroups[outputName]
	if !found {
		// No exact match, try to use all matching files (legacy behavior)
		matches, err := filepath.Glob(m.config.Pattern)
		if err != nil {
			return fmt.Errorf("failed to find matching files: %w", err)
		}
		filesToMerge = matches
	}

	// Sort files lexicographically (assumes zero-padded indexes)
	sort.Strings(filesToMerge)

	fmt.Printf("Merging %d chunk files into: %s\n", len(filesToMerge), outputName)

	// Create temporary output file
	tmpPath := outputName + ".assembling"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	var totalBytes int64

	// Merge all chunks
	for i, partPath := range filesToMerge {
		if err := m.mergeChunk(tmpFile, partPath, i+1, len(filesToMerge), &totalBytes); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return err
		}

		// Delete chunk if requested
		if m.config.Delete {
			if err := os.Remove(partPath); err != nil {
				fmt.Printf("Warning: failed to delete %s: %v\n", partPath, err)
			}
		}
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close output file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, outputName); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename output file: %w", err)
	}

	fmt.Printf("Merge complete: %s (%s)\n", outputName, formatBytes(totalBytes))

	// Delete state file if requested
	if m.config.Delete {
		stateFile := fmt.Sprintf(".%s-state.json", outputName)
		if err := os.Remove(stateFile); err != nil {
			if !os.IsNotExist(err) {
				fmt.Printf("Warning: failed to delete state file %s: %v\n", stateFile, err)
			}
		}
	}

	return nil
}

// mergeChunk copies a single chunk file to the output
func (m *Merger) mergeChunk(output *os.File, partPath string, current, total int, totalBytes *int64) error {
	fmt.Printf("[%d/%d] Merging %s\n", current, total, partPath)

	partFile, err := os.Open(partPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", partPath, err)
	}
	defer partFile.Close()

	n, err := io.Copy(output, partFile)
	if err != nil {
		return fmt.Errorf("failed to copy %s: %w", partPath, err)
	}

	*totalBytes += n

	return nil
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

// groupFilesByBasename groups .part files by their basename
// Example: ["file.000000.part", "file.000001.part", "other.000000.part"]
// Returns: {"file": ["file.000000.part", "file.000001.part"], "other": ["other.000000.part"]}
func groupFilesByBasename(partFiles []string) map[string][]string {
	groups := make(map[string][]string)

	for _, partFile := range partFiles {
		basename := extractBasename(partFile)
		if basename != "" {
			groups[basename] = append(groups[basename], partFile)
		}
	}

	return groups
}

// partFilePattern matches chunk files like "prefix.000000.part"
var partFilePattern = regexp.MustCompile(`^(.+?)\.(\d+)\.part$`)

// extractBasename extracts the basename from a .part file by stripping .part extension and numeric index
// Example: "file.000000.part" -> "file"
// Example: "download.123456.part" -> "download"
func extractBasename(partFile string) string {
	name := filepath.Base(partFile)

	matches := partFilePattern.FindStringSubmatch(name)
	if len(matches) == 3 {
		return matches[1] // Return the captured basename
	}

	// Fallback if pattern doesn't match
	return ""
}
