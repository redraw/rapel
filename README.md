# rapel

Chunked HTTP downloader with resume support.

A modern, cross-platform implementation with improved state management and progress tracking.

### Installation

```bash
go build -o bin/rapel
```

Or install directly:
```bash
go install github.com/redraw/rapel@latest
```

### Usage

Download a file with default settings (100MB chunks, 1 concurrent job):
```bash
rapel download https://example.com/file.bin
```

Download with custom chunk size and multiple concurrent downloads:
```bash
rapel download https://example.com/file.bin -c 50M --jobs 4
```

Download through a proxy:
```bash
rapel download https://example.com/file.bin -x socks5h://127.0.0.1:9050
```

Download and auto-merge:
```bash
rapel download https://example.com/file.bin --merge
```

Run command after each chunk completes:
```bash
rapel download https://example.com/file.bin --post-part 'rclone move {part} remote:bucket/'
```

Merge chunk files manually:
```bash
rapel merge                                    # Auto-detects output name
rapel merge --pattern 'file.*.part'           # Auto-detects as "file"
rapel merge -o output.bin --delete            # Explicit name, delete after merge
```

### Features

- **JSON state management**: Single `.rapel-state.json` file tracks all chunk metadata
- **Graceful shutdown**: Ctrl+C saves progress and allows resume
- **Better progress display**: Real-time speed, completion status with ANSI formatting
- **Cross-platform**: Works on Linux (amd64, arm64, arm v6/v7), macOS (Intel/Apple Silicon), Windows, and FreeBSD
- **Raspberry Pi support**: Native ARM v7 and v6 binaries for all Raspberry Pi models
- **Resume support**: Automatically resumes interrupted downloads
- **Concurrent downloads**: Download multiple chunks simultaneously
- **Post-part hooks**: Run custom commands after each chunk completes (e.g., upload to cloud)
- **Smart merging**: Auto-detects output filename and handles multiple download sessions

### Options

**Download command:**
```
-c SIZE          Chunk size (K, M, G suffix). Default: 100M
-x URL           Proxy URL (e.g., socks5h://127.0.0.1:9050)
-r N             Retries per request. Default: 10
--no-head        Skip HEAD request (requires --size)
--size BYTES     Total size in bytes (required if --no-head)
--jobs N         Concurrent chunks. Default: 1
--force          Force re-download even if state exists
--merge          Merge chunks after download (auto-detects output name)
--post-part CMD  Command to run after each part completes
                 Placeholders: {part} {idx} {base}
```

**Merge command:**
```
-o FILE        Output filename (auto-detected if not provided)
--pattern GLOB Pattern for chunk files. Default: *.part
--delete       Delete chunk files after merging
```
