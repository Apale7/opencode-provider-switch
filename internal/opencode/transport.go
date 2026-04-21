package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

const defaultModelDiscoveryTimeout = 20 * time.Second

type HTTPMiddleware func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error)

type TransportOptions struct {
	BaseURL         string
	Headers         map[string]string
	HTTPClient      *http.Client
	Middleware      []HTTPMiddleware
	RequestTimeout  time.Duration
	MaxRetries      int
	CaptureResponse bool
	ResponseHeaders map[string]string
	ResponseBody    *[]byte
}

type ProviderModelsError struct {
	Code         string
	Message      string
	StatusCode   int
	Retryable    bool
	ResponseBody string
	Headers      map[string]string
}

func (e *ProviderModelsError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func DoJSON(ctx context.Context, req *http.Request, opts TransportOptions) (*http.Response, []byte, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = defaultModelDiscoveryTimeout
	}
	attempts := opts.MaxRetries + 1
	if attempts <= 0 {
		attempts = 1
	}
	var chain func(index int, request *http.Request) (*http.Response, error)
	chain = func(index int, request *http.Request) (*http.Response, error) {
		if index >= len(opts.Middleware) {
			return client.Do(request)
		}
		return opts.Middleware[index](request, func(next *http.Request) (*http.Response, error) {
			return chain(index+1, next)
		})
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, opts.RequestTimeout)
		clone := req.Clone(attemptCtx)
		for key, value := range opts.Headers {
			if strings.TrimSpace(key) == "" {
				continue
			}
			clone.Header.Set(key, value)
		}
		resp, err := chain(0, clone)
		cancel()
		if err != nil {
			lastErr = &ProviderModelsError{Code: "request_failed", Message: fmt.Sprintf("request %s: %v", clone.URL.String(), err), Retryable: isRetryableError(err)}
			if attempt+1 < attempts && isRetryableError(err) {
				continue
			}
			return nil, nil, lastErr
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return resp, nil, fmt.Errorf("read %s: %w", clone.URL.String(), readErr)
		}
		if opts.ResponseBody != nil {
			*opts.ResponseBody = append((*opts.ResponseBody)[:0], body...)
		}
		if opts.ResponseHeaders != nil {
			for key := range opts.ResponseHeaders {
				delete(opts.ResponseHeaders, key)
			}
			for key, values := range resp.Header {
				if len(values) == 0 {
					continue
				}
				opts.ResponseHeaders[http.CanonicalHeaderKey(key)] = strings.Join(values, ", ")
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, body, nil
		}
		apiErr := &ProviderModelsError{
			Code:         classifyStatusCode(resp.StatusCode),
			StatusCode:   resp.StatusCode,
			Retryable:    isRetryableStatus(resp.StatusCode),
			ResponseBody: string(bytes.TrimSpace(body)),
			Headers:      cloneHeaderMap(resp.Header),
			Message:      unexpectedStatusMessage(clone.URL.String(), resp.Status, body),
		}
		lastErr = apiErr
		if attempt+1 < attempts && apiErr.Retryable {
			continue
		}
		return resp, body, apiErr
	}
	if lastErr != nil {
		return nil, nil, lastErr
	}
	return nil, nil, fmt.Errorf("request failed without response")
}

func newProviderModelsRequest(protocol string, baseURL string, apiKey string, headers map[string]string) (*http.Request, error) {
	protocol = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + config.ProtocolUpstreamModelsPath(protocol)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	config.ApplyProtocolAuthHeaders(req.Header, protocol, apiKey)
	config.ApplyProtocolDefaultHeaders(req.Header, protocol)
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	return req, nil
}

func classifyStatusCode(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth_failed"
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return "retryable_status"
	default:
		if status >= 500 {
			return "retryable_status"
		}
		return "bad_status"
	}
}

func isRetryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errorsAs(err, &netErr) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func cloneHeaderMap(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		out[http.CanonicalHeaderKey(key)] = strings.Join(values, ", ")
	}
	return out
}

func unexpectedStatusMessage(url string, status string, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Sprintf("request %s: unexpected status %s", url, status)
	}
	return fmt.Sprintf("request %s: unexpected status %s: %s", url, status, trimmed)
}

func sortedHeaderKeys(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
