package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultTraceLimit = 200

type TraceStore struct {
	mu     sync.Mutex
	limit  int
	traces []RequestTrace
}

type RequestTrace struct {
	ID             uint64            `json:"id"`
	StartedAt      time.Time         `json:"startedAt"`
	FinishedAt     time.Time         `json:"finishedAt,omitempty"`
	DurationMs     int64             `json:"durationMs"`
	FirstByteMs    int64             `json:"firstByteMs,omitempty"`
	InputTokens    int64             `json:"inputTokens,omitempty"`
	OutputTokens   int64             `json:"outputTokens,omitempty"`
	Protocol       string            `json:"protocol,omitempty"`
	RawModel       string            `json:"rawModel,omitempty"`
	Alias          string            `json:"alias,omitempty"`
	Stream         bool              `json:"stream"`
	Success        bool              `json:"success"`
	StatusCode     int               `json:"statusCode,omitempty"`
	Error          string            `json:"error,omitempty"`
	FinalProvider  string            `json:"finalProvider,omitempty"`
	FinalModel     string            `json:"finalModel,omitempty"`
	FinalURL       string            `json:"finalUrl,omitempty"`
	Failover       bool              `json:"failover"`
	AttemptCount   int               `json:"attemptCount"`
	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`
	RequestParams  any               `json:"requestParams,omitempty"`
	Attempts       []TraceAttempt    `json:"attempts"`
}

type TraceAttempt struct {
	Attempt         int               `json:"attempt"`
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	URL             string            `json:"url,omitempty"`
	StartedAt       time.Time         `json:"startedAt"`
	DurationMs      int64             `json:"durationMs"`
	FirstByteMs     int64             `json:"firstByteMs,omitempty"`
	StatusCode      int               `json:"statusCode,omitempty"`
	Success         bool              `json:"success"`
	Retryable       bool              `json:"retryable"`
	Skipped         bool              `json:"skipped"`
	Result          string            `json:"result,omitempty"`
	Error           string            `json:"error,omitempty"`
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`
	RequestParams   any               `json:"requestParams,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    string            `json:"responseBody,omitempty"`
}

func NewTraceStore(limit int) *TraceStore {
	if limit <= 0 {
		limit = defaultTraceLimit
	}
	return &TraceStore{limit: limit}
}

func (s *TraceStore) Add(trace RequestTrace) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := cloneTrace(trace)
	s.traces = append([]RequestTrace{clone}, s.traces...)
	if len(s.traces) > s.limit {
		s.traces = s.traces[:s.limit]
	}
}

func (s *TraceStore) List(limit int) []RequestTrace {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.traces) {
		limit = len(s.traces)
	}
	out := make([]RequestTrace, 0, limit)
	for _, trace := range s.traces[:limit] {
		out = append(out, cloneTrace(trace))
	}
	return out
}

func cloneTrace(trace RequestTrace) RequestTrace {
	trace.RequestHeaders = cloneStringMap(trace.RequestHeaders)
	trace.RequestParams = cloneJSONValue(trace.RequestParams)
	if len(trace.Attempts) == 0 {
		trace.Attempts = []TraceAttempt{}
		return trace
	}
	trace.Attempts = append([]TraceAttempt(nil), trace.Attempts...)
	for index := range trace.Attempts {
		trace.Attempts[index].RequestHeaders = cloneStringMap(trace.Attempts[index].RequestHeaders)
		trace.Attempts[index].RequestParams = cloneJSONValue(trace.Attempts[index].RequestParams)
		trace.Attempts[index].ResponseHeaders = cloneStringMap(trace.Attempts[index].ResponseHeaders)
	}
	return trace
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = cloneJSONValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, nested := range typed {
			out[index] = cloneJSONValue(nested)
		}
		return out
	default:
		return typed
	}
}

var sensitiveHeaderNames = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
	"cookie":              true,
	"set-cookie":          true,
}

var redactedPayloadKeys = map[string]bool{
	"content":      true,
	"input":        true,
	"instructions": true,
	"messages":     true,
	"output":       true,
	"output_text":  true,
	"prompt":       true,
	"response":     true,
	"text":         true,
}

func sanitizeHeaderMap(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		values := header.Values(key)
		joined := strings.Join(values, ", ")
		if sensitiveHeaderNames[strings.ToLower(key)] {
			joined = maskSensitiveValue(joined)
		}
		out[key] = joined
	}
	return out
}

func sanitizeJSONValue(key string, value any) any {
	if redactedPayloadKeys[strings.ToLower(strings.TrimSpace(key))] {
		return redactedSummary(value)
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for nestedKey := range typed {
			keys = append(keys, nestedKey)
		}
		sort.Strings(keys)
		for _, nestedKey := range keys {
			out[nestedKey] = sanitizeJSONValue(nestedKey, typed[nestedKey])
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, nested := range typed {
			out[index] = sanitizeJSONValue("", nested)
		}
		return out
	default:
		return typed
	}
}

func redactedSummary(value any) any {
	switch typed := value.(type) {
	case []any:
		return fmt.Sprintf("<redacted %d item(s)>", len(typed))
	case map[string]any:
		return fmt.Sprintf("<redacted object with %d key(s)>", len(typed))
	case string:
		if typed == "" {
			return "<redacted>"
		}
		return fmt.Sprintf("<redacted %d chars>", len(typed))
	default:
		return "<redacted>"
	}
}

func sanitizeResponseBody(contentType string, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		var payload any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			sanitized := sanitizeJSONValue("", payload)
			encoded, err := json.Marshal(sanitized)
			if err == nil {
				return truncate(string(encoded), 200)
			}
		}
	}
	if redacted, ok := redactedSummary(trimmed).(string); ok {
		return redacted
	}
	return "<redacted>"
}

func maskSensitiveValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "***"
	}
	if len(trimmed) <= 8 {
		return "***"
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}
