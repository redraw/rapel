# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`rapel` is a Go-based toolkit for downloading large files via chunked HTTP Range requests and reassembling them. The project consists of two subcommands:

- `rapel download`: Main downloader using HTTP Range requests, concurrent downloads, and resume capability
- `rapel merge`: Utility to concatenate chunk files in order

**Module**: `github.com/redraw/rapel`

## Architecture

### Project Structure

```
cmd/
  download.go     - Download subcommand implementation
  merge.go        - Merge subcommand implementation
internal/
  downloader/
    downloader.go - Core download logic with worker pool
    chunk.go      - Chunk file management (.tmp, .part files)
    progress.go   - Progress tracking and display
    state.go      - Download state persistence
  merger/
    merger.go     - Chunk file merging with basename grouping
  http/
    client.go     - HTTP client with retry logic
main.go           - CLI entry point
```

### Download Command (cmd/download.go)

**Flags** (must be specified BEFORE the URL):
- `-c SIZE`: Chunk size (K, M, G suffix). Default: 100M
- `-x URL`: Proxy URL (e.g., socks5h://127.0.0.1:9050)
- `-r N`: Retries per request. Default: 10
- `--no-head`: Skip HEAD request (requires --size)
- `--size BYTES`: Total size in bytes (required if --no-head)
- `--jobs N`: Concurrent chunks. Default: 1
- `--force`: Force re-download even if state exists
- `--merge`: Merge chunks after download (auto-detects output name)
- `--post-part CMD`: Command to run after each part completes

**Important**: Due to Go's standard `flag` package behavior, all flags must be specified BEFORE the URL argument. Flags after the URL will be ignored.

**State Management** (internal/downloader/state.go):
- Uses a file-based state model in the current directory:
  - `<prefix>.XXXXXX.tmp`: Download in progress
  - `<prefix>.XXXXXX.part`: Completed chunk
  - State is persisted as JSON for resume capability

**Core Flow** (internal/downloader/downloader.go:110-170):
1. Worker pool pattern with configurable concurrency
2. Each chunk is downloaded by a goroutine
3. Resume support: checks for existing `.tmp` files and resumes from current size
4. On completion: renames `.tmp` → `.part`, marks chunk as completed
5. Optional `--post-part` hook runs in separate goroutine after each chunk completes
6. Optional `--merge` calls merger with auto-detected output name

**Post-Part Hook** (internal/downloader/downloader.go:265-282):
- Executes command in a goroutine (non-blocking)
- Placeholder substitution:
  - `{part}`: Path to completed .part file
  - `{idx}`: Chunk index (integer)
  - `{base}`: Filename prefix
- Example: `--post-part 'rclone move {part} remote:bucket/'`

### Merge Command (cmd/merge.go)

**Flags**:
- `-o FILE`: Output filename (auto-detected if not provided)
- `--pattern GLOB`: Pattern for chunk files. Default: *.part
- `--delete`: Delete chunk files and state file after merging

**Basename Grouping** (internal/merger/merger.go:129-140):
- Groups .part files by extracting basename using regex: `^(.+?)\.(\d+)\.part$`
- Example: `file.000000.part`, `file.000001.part` → group `file`
- If multiple basenames found and no `-o` specified, merges all groups into separate output files
- If single basename found, auto-detects output name

**Merge Process** (internal/merger/merger.go:32-99):
1. Find all files matching pattern
2. Group by basename
3. Determine which files to merge (auto-detect or user-specified)
4. Sort files lexicographically (relies on zero-padded indexes)
5. Concatenate into temporary file `${OUTPUT}.assembling`
6. Atomic rename to final output on success
7. Optional deletion of chunks after each is appended
8. Optional deletion of state file after successful merge (if `--delete` flag)

## Common Commands

### Download a file in chunks
```bash
rapel download https://example.com/file.bin
# Uses defaults: 100M chunks, 1 concurrent job, 10 retries
```

### Resume interrupted download
```bash
rapel download https://example.com/file.bin
# Automatically resumes based on state file and .tmp partial files
```

### Concurrent download with custom chunk size
```bash
rapel download -c 50M --jobs 4 https://example.com/file.bin
# Note: flags MUST come before the URL
```

### Download with proxy (e.g., Tor)
```bash
rapel download -x socks5h://127.0.0.1:9050 https://example.com/file.bin
```

### Force re-download
```bash
rapel download --force https://example.com/file.bin
```

### Download and auto-merge
```bash
rapel download --merge https://example.com/file.bin
# Automatically merges into output file after download completes
# Output name is auto-detected by stripping .part and numeric index
```

### Post-processing hook (e.g., upload completed chunks)
```bash
rapel download --post-part 'rclone move {part} remote:bucket/' https://example.com/file.bin
# Placeholders: {part} {idx} {base}
# Runs in goroutine (non-blocking)
```

### Merge chunks into final file (auto-detect)
```bash
rapel merge
# Auto-detects output name from *.part files
# If multiple basenames found, merges all groups into separate files
```

### Merge specific chunks
```bash
rapel merge --pattern 'file.*.part'
# Auto-detects output name as "file"
```

### Merge with explicit output name
```bash
rapel merge -o output.bin --pattern 'file.*.part'
# Merges only chunks matching the "file" basename
```

### Merge specific chunks and delete them
```bash
rapel merge --pattern 'file.*.part' --delete
# Deletes both chunk files and state file (.file-state.json) after merge
```

## Testing

Manual testing approach:
1. Test download with small file and small chunks: `rapel download <url> -c 1M --jobs 2`
2. Verify chunk files created: `ls *.part`
3. Interrupt download (Ctrl+C) and resume: verify `.tmp` files resume correctly
4. Test merge: `rapel merge && diff output <original>`
5. Test with `--force` flag to verify re-download behavior
6. Test `--merge` flag to verify auto-assembly
7. Test `--post-part` hook with a simple command like `echo {part} >> log.txt`
8. Test merge with multiple basenames to verify grouping logic

## Implementation Notes

### Download Implementation (internal/downloader/)

**Concurrency** (downloader.go:110-170):
- Uses worker pool pattern with semaphore for concurrency control
- Each chunk downloads in its own goroutine
- Context-based cancellation for graceful shutdown
- Errors in any chunk cancel all other downloads

**Resume Logic** (chunk.go):
- `OpenChunkFile()` checks for existing `.tmp` file
- Uses `os.Stat()` to get current file size
- Resumes download from `chunk.Start + currentSize`
- On completion, renames `.tmp` to `.part`

**HTTP Client** (internal/http/client.go):
- Uses `net/http.Client` with configurable timeouts
- Default retry logic: 10 retries with exponential backoff
- Supports proxy configuration via `http.ProxyFromEnvironment` or explicit proxy URL
- Uses `Range` header for partial downloads

### Merge Implementation (internal/merger/merger.go)

**Basename Extraction** (merger.go:142-158):
- Uses regex pattern: `^(.+?)\.(\d+)\.part$`
- Non-greedy capture of basename to handle dots in filenames
- Returns empty string if pattern doesn't match

**Grouping Logic** (merger.go:129-140):
- Creates map of basename → list of files
- Enables merging only files from specific download session
- Prevents mixing chunks from different downloads

**File Handling**:
- Lexicographic sort assumes zero-padded indexes
- Uses `.assembling` temporary file for atomic writes
- Chunk deletion (if `--delete` flag) happens after each chunk is appended
- State file deletion (if `--delete` flag) happens after successful merge
- Atomic rename to final output prevents partial files

### State Consistency

**Download State**:
- State is persisted as JSON after each chunk completes
- Contains URL, total size, chunk size, filename prefix, and chunk status
- State file location: current directory (derived from URL)
- On resume, validates URL and size match existing state

**Chunk Files**:
- `.tmp` files: Downloads in progress, can be resumed
- `.part` files: Completed chunks, ready for merging
- Missing state but existing `.tmp`/`.part` files: can still be merged manually

### Error Handling

- HTTP errors (4xx, 5xx) are retried according to retry configuration
- Network errors trigger exponential backoff
- Context cancellation (Ctrl+C) saves state before exit
- Merge errors remove `.assembling` temporary file

### Performance Considerations

- Chunk size affects memory usage and parallelism granularity
- Larger chunks = fewer HTTP requests, less overhead
- Smaller chunks = better parallelism, easier resume
- Default 100M is a good balance for most use cases
- Post-part hooks run in goroutines to avoid blocking downloads
