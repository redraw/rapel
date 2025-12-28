.PHONY: build build-all clean install test fmt vet

# Build for current platform
build:
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/rapel .

# Build for all platforms
build-all: clean
	@mkdir -p dist
	# Linux
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/rapel-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/rapel-linux-arm64 .
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o dist/rapel-linux-armv7 .
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags="-s -w" -o dist/rapel-linux-armv6 .
	# macOS
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dist/rapel-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/rapel-darwin-arm64 .
	# Windows
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/rapel-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -ldflags="-s -w" -o dist/rapel-windows-arm64.exe .
	# FreeBSD
	GOOS=freebsd GOARCH=amd64 go build -ldflags="-s -w" -o dist/rapel-freebsd-amd64 .
	@echo "Built all binaries in dist/"

# Clean build artifacts
clean:
	rm -rf bin/ dist/ dist-all/ release/

# Install to GOPATH/bin
install:
	go install -ldflags="-s -w" .

# Run tests (when tests exist)
test:
	go test -v ./...

# Format code
fmt:
	gofmt -s -w .

# Run go vet
vet:
	go vet ./...

# Run all checks
check: fmt vet
	@echo "All checks passed"
