package http

import (
	"context"
	"fmt"
	"io"
	"math"
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

// DownloadRange downloads a byte range with retry logic
func (c *Client) DownloadRange(ctx context.Context, url string, start, end int64, writer io.Writer) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2^attempt seconds, max 60s
			backoffSecs := math.Min(math.Pow(2, float64(attempt)), 60)
			backoff := time.Duration(backoffSecs) * time.Second

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := c.downloadRangeOnce(ctx, url, start, end, writer)
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("download failed after %d retries: %w", c.config.MaxRetries, lastErr)
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

	// Copy with context cancellation check
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write failed: %w", writeErr)
			}
		}

		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}
	}
}
