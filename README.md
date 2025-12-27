# httpchunk
```
httpchunk - Download a single HTTP/HTTPS URL into numbered chunk files using Range requests.

State model (files in current directory):
  <name>.XXXXXX.tmp   -> in progress
  <name>.XXXXXX.part  -> completed chunk
  <name>.XXXXXX.done  -> completion marker (source of truth)

Usage:
  httpchunk URL [options]

Options:
  -c CHUNK_SIZE     Chunk size (K, M, G decimal). Default: 100M
  -x PROXY          curl proxy URL
  -r RETRIES        Retries per request. Default: 10
  --no-head         Skip HEAD (requires --size)
  --size BYTES      Total size in bytes (required if --no-head)
  --index-width N   Digits for chunk index. Default: 6
  --jobs N          Concurrent chunks. Default: 1
  --force           Re-download even if .done exists
  --post-part CMD   Run after a chunk completes.
                    Placeholders: {part} {done} {idx} {base}
  --assemble FILE   Concatenate all *.part into FILE
  --help            Show this help
```

## Upload while download

```bash
while true; do
  find . -maxdepth 1 -name '*.part' -print -quit | grep -q . && rclone move . r2:tmp/ --include='*.part' --max-depth 1 -v;
  sleep 5;
done
```

## Merge parts (and delete parts while merging)
```bash
./mergeparts -o <OUTPUT> --pattern "*.parts" --delete
```
