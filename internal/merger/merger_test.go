package merger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractBasename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple basename", input: "file.000000.part", expected: "file"},
		{name: "basename with dots", input: "my.file.name.000001.part", expected: "my.file.name"},
		{name: "large index", input: "download.123456.part", expected: "download"},
		{name: "with path", input: "/path/to/file.000000.part", expected: "file"},
		{name: "invalid format - no index", input: "file.part", expected: ""},
		{name: "invalid format - no extension", input: "file.000000", expected: ""},
		{name: "invalid format - wrong extension", input: "file.000000.tmp", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractBasename(tt.input))
		})
	}
}

func TestGroupFilesByBasename(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string][]string
	}{
		{
			name:  "single group",
			input: []string{"file.000000.part", "file.000001.part", "file.000002.part"},
			expected: map[string][]string{
				"file": {"file.000000.part", "file.000001.part", "file.000002.part"},
			},
		},
		{
			name:  "multiple groups",
			input: []string{"file1.000000.part", "file1.000001.part", "file2.000000.part", "file2.000001.part"},
			expected: map[string][]string{
				"file1": {"file1.000000.part", "file1.000001.part"},
				"file2": {"file2.000000.part", "file2.000001.part"},
			},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: map[string][]string{},
		},
		{
			name:  "invalid files filtered out",
			input: []string{"file.000000.part", "invalid.part", "file.000001.part"},
			expected: map[string][]string{
				"file": {"file.000000.part", "file.000001.part"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groupFilesByBasename(tt.input)
			assert.Equal(t, len(tt.expected), len(result))
			for basename, expectedFiles := range tt.expected {
				assert.ElementsMatch(t, expectedFiles, result[basename])
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{name: "bytes", input: 500, expected: "500 B"},
		{name: "kilobytes", input: 1500, expected: "1.5 KB"},
		{name: "megabytes", input: 1500000, expected: "1.5 MB"},
		{name: "gigabytes", input: 1500000000, expected: "1.5 GB"},
		{name: "terabytes", input: 1500000000000, expected: "1.5 TB"},
		{name: "zero", input: 0, expected: "0 B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatBytes(tt.input))
		})
	}
}
