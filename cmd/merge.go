package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/redraw/rapel/internal/merger"
)

// MergeCommand implements the merge subcommand
func MergeCommand(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)

	// Define flags
	output := fs.String("o", "", "Output filename (auto-detected if not provided)")
	pattern := fs.String("pattern", "*.part", "Pattern for chunk files")
	delete := fs.Bool("delete", false, "Delete chunks after merging")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: rapel merge [options]

Merge chunk files into output file(s).
If multiple .part groups are found, all groups are merged into separate files.

Options:
  -o FILE        Output filename (auto-detected from pattern if not provided)
  --pattern GLOB Pattern for chunk files. Default: *.part
  --delete       Delete chunk files after merging

Examples:
  rapel merge                              # Merge all .part groups
  rapel merge -o file.bin                  # Merge specific group
  rapel merge --pattern 'file.*.part'      # Merge group matching pattern
  rapel merge --pattern 'file.*.part' --delete
`)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create merger
	m := merger.NewMerger(merger.Config{
		Output:  *output,
		Pattern: *pattern,
		Delete:  *delete,
	})

	// Perform merge
	if err := m.Merge(); err != nil {
		return err
	}

	return nil
}
