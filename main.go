package main

import (
	"fmt"
	"os"

	"github.com/redraw/rapel/cmd"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "download":
		if err := cmd.DownloadCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "merge":
		if err := cmd.MergeCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "version", "--version", "-v":
		fmt.Printf("rapel version %s\n", version)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `rapel - Chunked HTTP downloader with resume support

Usage:
  rapel <command> [options]

Commands:
  download    Download a file using chunked HTTP Range requests
  merge       Merge chunk files into a single file
  version     Show version information
  help        Show this help message

Run 'rapel <command> --help' for more information on a command.

Examples:
  rapel download https://example.com/file.bin
  rapel download -c 50M --jobs 4 https://example.com/file.bin
  rapel merge -o output.bin --pattern 'file.*.part'

For more information, visit: https://github.com/redraw/rapel
`)
}
