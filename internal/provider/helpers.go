package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

// Shared HTTP client for downloading media URLs.
var downloadClient = &http.Client{Timeout: 60 * time.Second}

// downloadAndEncode fetches a URL and returns its content as base64.
func downloadAndEncode(ctx context.Context, url string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d fetching URL", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// httpError reads the response body and returns a ProviderError for non-OK status codes.
// Returns nil if the status is acceptable (2xx).
func httpError(provider string, resp *http.Response) *models.ProviderError {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return &models.ProviderError{
		Provider:  provider,
		Message:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
		Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500,
	}
}

// doJSON marshals payload as JSON, POSTs to url with the given headers, and
// decodes the response into result. Returns the raw *http.Response for
// callers that need the body as bytes (pass nil for result in that case).
func doJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, payload any, result any) (*http.Response, error) {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	// When decoding into result, check HTTP status first and return error body.
	if result != nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return resp, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return resp, fmt.Errorf("decode response: %w", err)
		}
	}

	return resp, nil
}

// pollForCompletion polls a status URL until isDone returns true or maxAttempts is reached.
func pollForCompletion(ctx context.Context, client *http.Client, headers map[string]string, statusURL string, interval time.Duration, maxAttempts int, checkStatus func(body []byte) (done bool, err error)) error {
	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			continue // retry transient errors
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		done, err := checkStatus(body)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return fmt.Errorf("timed out after %d poll attempts", maxAttempts)
}

// Parameter helpers

func getParam(params map[string]any, key, defaultVal string) string {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getParamFloat(params map[string]any, key string, defaultVal float64) float64 {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		}
	}
	return defaultVal
}

func getParamInt(params map[string]any, key string, defaultVal int) int {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case float32:
			return int(n)
		}
	}
	return defaultVal
}

// isTransientError checks whether an error is likely transient and worth retrying.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientPatterns := []string{
		"rate limit", "429", "500", "502", "503", "504",
		"timeout", "connection reset", "connection refused",
		"eof", "broken pipe", "temporary failure",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}
