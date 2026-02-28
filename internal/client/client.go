package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CreditMeta holds credit usage information extracted from API response headers.
type CreditMeta struct {
	CreditsUsed      *int `json:"credits_used,omitempty"`
	CreditsRemaining *int `json:"credits_remaining,omitempty"`
}

// Response wraps a successful API response with extracted credit metadata.
type Response struct {
	StatusCode int
	Body       []byte
	Credits    CreditMeta
}

// Options configures the Client behavior per-session.
type Options struct {
	APIKey  string
	BaseURL string
	Timeout time.Duration
	NoRetry bool
	Version string
}

// Client is the shared HTTP client for all Shovels API calls. It handles
// authentication, credit header extraction, retries with jittered exponential
// backoff, and structured error translation.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	noRetry    bool
	version    string
	maxRetries int

	// sleepFn is the function used for retry delays. Defaults to time.Sleep
	// but can be overridden in tests.
	sleepFn func(time.Duration)
}

// New creates a Client from the given options. The returned client is safe
// for concurrent use across multiple commands.
func New(opts Options) *Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	version := opts.Version
	if version == "" {
		version = "dev"
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		apiKey:     opts.APIKey,
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		noRetry:    opts.NoRetry,
		version:    version,
		maxRetries: 3,
		sleepFn:    time.Sleep,
	}
}

// Get performs an authenticated GET request to the given API path (relative to
// baseURL) with the provided query parameters. It returns the parsed response
// or an *APIError on failure.
func (c *Client) Get(ctx context.Context, path string, query map[string]string) (*Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, networkError(err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("User-Agent", "shovels-cli/"+c.version)

	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	return c.doWithRetry(ctx, req)
}

// doWithRetry executes the request, retrying on 429 responses with jittered
// exponential backoff unless --no-retry is set.
func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*Response, error) {
	var lastErr *APIError

	attempts := 1 + c.maxRetries
	if c.noRetry {
		attempts = 1
	}

	for attempt := range attempts {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Transport-level error (timeout, DNS, etc.)
			return nil, networkError(err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, networkError(readErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			credits := extractCredits(resp.Header)
			return &Response{
				StatusCode: resp.StatusCode,
				Body:       body,
				Credits:    credits,
			}, nil
		}

		apiErr := statusToError(resp.StatusCode, extractErrorMessage(body))

		if resp.StatusCode != 429 {
			return nil, apiErr
		}

		// 429: retry unless disabled or max attempts reached.
		lastErr = apiErr
		if attempt < attempts-1 {
			delay := retryDelay(attempt, resp.Header)
			select {
			case <-ctx.Done():
				return nil, networkError(ctx.Err())
			default:
				c.sleepFn(delay)
			}
		}
	}

	// All retries exhausted for 429.
	if c.noRetry {
		return nil, lastErr
	}
	return nil, &APIError{
		Message:   fmt.Sprintf("Rate limited after %d retries", c.maxRetries),
		ExitCode:  ExitRateLimit,
		ErrorType: ErrorTypeRateLimit,
	}
}

// retryDelay computes the delay for the given retry attempt using jittered
// exponential backoff: base * 2^attempt +/- 25%. If the Retry-After header
// contains a valid integer, that value (in seconds) is used instead.
func retryDelay(attempt int, header http.Header) time.Duration {
	if ra := header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}

	baseSeconds := math.Pow(2, float64(attempt))
	jitter := 0.75 + rand.Float64()*0.5 // [0.75, 1.25] range gives +/- 25%
	return time.Duration(baseSeconds*jitter*1000) * time.Millisecond
}

// extractCredits reads credit-related headers from the API response.
// Missing headers result in nil pointer values (null in JSON).
func extractCredits(header http.Header) CreditMeta {
	var meta CreditMeta

	if v := header.Get("X-Credits-Request"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			meta.CreditsUsed = &n
		}
	}
	if v := header.Get("X-Credits-Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			meta.CreditsRemaining = &n
		}
	}

	return meta
}

// extractErrorMessage attempts to extract a human-readable error message
// from a JSON response body. Falls back to the raw body text if JSON parsing
// fails. Returns empty string if body is empty.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	// Try common API error response shapes.
	var structured struct {
		Detail any    `json:"detail"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &structured); err == nil {
		if structured.Error != "" {
			return structured.Error
		}
		if structured.Detail != nil {
			switch v := structured.Detail.(type) {
			case string:
				return v
			default:
				// For 422 validation errors, detail may be an array of objects.
				detailBytes, _ := json.Marshal(v)
				return string(detailBytes)
			}
		}
	}

	return strings.TrimSpace(string(body))
}
