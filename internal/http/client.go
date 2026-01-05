package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Config holds HTTP client configuration
type Config struct {
	ProxyURL       string
	MaxRetries     int
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

// Client wraps http.Client with retry logic
type Client struct {
	client *http.Client
	config Config
}

// NewClient creates a new HTTP client with the given configuration
func NewClient(config Config) (*Client, error) {
	transport := &http.Transport{
		ResponseHeaderTimeout: config.ReadTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	// Configure proxy if provided
	if config.ProxyURL != "" {
		proxyURL, err := url.Parse(config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.ConnectTimeout,
	}

	return &Client{
		client: client,
		config: config,
	}, nil
}

// GetContentLength performs a HEAD request to get the content length
func (c *Client) GetContentLength(ctx context.Context, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create HEAD request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HEAD request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HEAD request returned status %d", resp.StatusCode)
	}

	if resp.ContentLength <= 0 {
		return 0, fmt.Errorf("server did not provide content length")
	}

	return resp.ContentLength, nil
}

// DownloadRange downloads a byte range (no retry, caller handles retries)
func (c *Client) DownloadRange(ctx context.Context, url string, start, end int64, writer io.Writer) error {
	return c.downloadRangeOnce(ctx, url, start, end, writer)
}

// downloadRangeOnce attempts to download a byte range once
func (c *Client) downloadRangeOnce(ctx context.Context, url string, start, end int64, writer io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set Range header
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 206 (Partial Content) and 200 (OK)
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Calculate expected bytes to enforce download limit
	expectedBytes := end - start + 1

	// Copy with context cancellation check and byte limit enforcement
	buf := make([]byte, 32*1024)
	var totalRead int64
	for totalRead < expectedBytes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Calculate how many bytes to read in this iteration
		remaining := expectedBytes - totalRead
		readSize := len(buf)
		if remaining < int64(readSize) {
			readSize = int(remaining)
		}

		n, err := resp.Body.Read(buf[:readSize])
		if n > 0 {
			totalRead += int64(n)
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write failed: %w", writeErr)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}
	}

	// Verify we received the expected number of bytes
	if totalRead != expectedBytes {
		return fmt.Errorf("incomplete download: expected %d bytes, got %d", expectedBytes, totalRead)
	}

	return nil
}
