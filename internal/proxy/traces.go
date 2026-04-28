package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultTraceLimit = 200

const (
	defaultTracePage     = 1
	defaultTracePageSize = 25
	maxTracePageSize     = 100
)

type RequestTraceStore interface {
	Add(ctx context.Context, trace RequestTrace) error
	List(ctx context.Context, limit int) ([]RequestTrace, error)
	Query(ctx context.Context, query TraceQuery) (TraceQueryResult, error)
	Get(ctx context.Context, id uint64) (RequestTrace, bool, error)
	Close() error
}

type TraceQuery struct {
	Page           int
	PageSize       int
	Aliases        []string
	FailoverCounts []int
	StatusCodes    []int
	StartedFrom    time.Time
	StartedTo      time.Time
}

type TraceStats struct {
	Success  int
	Failover int
	Failed   int
}

type TraceQueryResult struct {
	Items                   []RequestTrace
	Total                   int
	Page                    int
	PageSize                int
	AvailableAliases        []string
	AvailableFailoverCounts []int
	AvailableStatusCodes    []int
	Stats                   TraceStats
}

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
	Usage          TraceUsage        `json:"usage,omitempty"`
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

type TraceUsage struct {
	RawInputTokens     *int64   `json:"rawInputTokens,omitempty"`
	RawOutputTokens    *int64   `json:"rawOutputTokens,omitempty"`
	RawTotalTokens     *int64   `json:"rawTotalTokens,omitempty"`
	InputTokens        *int64   `json:"inputTokens,omitempty"`
	OutputTokens       *int64   `json:"outputTokens,omitempty"`
	ReasoningTokens    *int64   `json:"reasoningTokens,omitempty"`
	CacheReadTokens    *int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens   *int64   `json:"cacheWriteTokens,omitempty"`
	CacheWrite1HTokens *int64   `json:"cacheWrite1hTokens,omitempty"`
	Source             string   `json:"source,omitempty"`
	Precision          string   `json:"precision,omitempty"`
	Notes              []string `json:"notes,omitempty"`
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

func (s *TraceStore) Add(ctx context.Context, trace RequestTrace) error {
	_ = ctx
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := cloneTrace(trace)
	s.traces = append([]RequestTrace{clone}, s.traces...)
	if len(s.traces) > s.limit {
		s.traces = s.traces[:s.limit]
	}
	return nil
}

func (s *TraceStore) List(ctx context.Context, limit int) ([]RequestTrace, error) {
	_ = ctx
	if s == nil {
		return nil, nil
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
	return out, nil
}

func (s *TraceStore) Query(ctx context.Context, query TraceQuery) (TraceQueryResult, error) {
	_ = ctx
	query = normalizeTraceQuery(query)
	if s == nil {
		return TraceQueryResult{Page: query.Page, PageSize: query.PageSize}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	timeScoped := make([]RequestTrace, 0, len(s.traces))
	for _, trace := range s.traces {
		if traceMatchesTimeRange(trace, query) {
			timeScoped = append(timeScoped, trace)
		}
	}
	filtered := make([]RequestTrace, 0, len(s.traces))
	for _, trace := range timeScoped {
		if traceMatchesQuery(trace, query) {
			filtered = append(filtered, traceSummary(trace))
		}
	}
	total := len(filtered)
	start := (query.Page - 1) * query.PageSize
	if start > total {
		start = total
	}
	end := start + query.PageSize
	if end > total {
		end = total
	}
	return TraceQueryResult{
		Items:                   filtered[start:end],
		Total:                   total,
		Page:                    query.Page,
		PageSize:                query.PageSize,
		AvailableAliases:        collectAvailableAliases(timeScoped),
		AvailableFailoverCounts: collectAvailableFailoverCounts(timeScoped),
		AvailableStatusCodes:    collectAvailableStatusCodes(timeScoped),
		Stats:                   collectTraceStats(timeScoped),
	}, nil
}

func (s *TraceStore) Get(ctx context.Context, id uint64) (RequestTrace, bool, error) {
	_ = ctx
	if s == nil || id == 0 {
		return RequestTrace{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, trace := range s.traces {
		if trace.ID == id {
			return cloneTrace(trace), true, nil
		}
	}
	return RequestTrace{}, false, nil
}

func (s *TraceStore) Close() error {
	return nil
}

func traceSummary(trace RequestTrace) RequestTrace {
	trace.RequestHeaders = nil
	trace.RequestParams = nil
	trace.Attempts = []TraceAttempt{}
	trace.Usage = cloneTraceUsage(trace.Usage)
	return trace
}

func cloneTrace(trace RequestTrace) RequestTrace {
	trace.RequestHeaders = cloneStringMap(trace.RequestHeaders)
	trace.RequestParams = cloneJSONValue(trace.RequestParams)
	trace.Usage = cloneTraceUsage(trace.Usage)
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

func cloneTraceUsage(in TraceUsage) TraceUsage {
	return TraceUsage{
		RawInputTokens:     cloneInt64Ptr(in.RawInputTokens),
		RawOutputTokens:    cloneInt64Ptr(in.RawOutputTokens),
		RawTotalTokens:     cloneInt64Ptr(in.RawTotalTokens),
		InputTokens:        cloneInt64Ptr(in.InputTokens),
		OutputTokens:       cloneInt64Ptr(in.OutputTokens),
		ReasoningTokens:    cloneInt64Ptr(in.ReasoningTokens),
		CacheReadTokens:    cloneInt64Ptr(in.CacheReadTokens),
		CacheWriteTokens:   cloneInt64Ptr(in.CacheWriteTokens),
		CacheWrite1HTokens: cloneInt64Ptr(in.CacheWrite1HTokens),
		Source:             in.Source,
		Precision:          in.Precision,
		Notes:              append([]string(nil), in.Notes...),
	}
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
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

func normalizeTraceQuery(query TraceQuery) TraceQuery {
	if query.Page <= 0 {
		query.Page = defaultTracePage
	}
	if query.PageSize <= 0 {
		query.PageSize = defaultTracePageSize
	}
	if query.PageSize > maxTracePageSize {
		query.PageSize = maxTracePageSize
	}
	query.Aliases = normalizeTraceAliases(query.Aliases)
	query.FailoverCounts = normalizeTraceInts(query.FailoverCounts, true)
	query.StatusCodes = normalizeTraceInts(query.StatusCodes, false)
	return query
}

func normalizeTraceAliases(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeTraceInts(in []int, allowZero bool) []int {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, item := range in {
		if item < 0 || (!allowZero && item == 0) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func traceMatchesQuery(trace RequestTrace, query TraceQuery) bool {
	if !traceMatchesTimeRange(trace, query) {
		return false
	}
	if len(query.Aliases) > 0 && !containsTraceAlias(query.Aliases, trace.Alias) {
		return false
	}
	if len(query.FailoverCounts) > 0 && !containsTraceInt(query.FailoverCounts, traceFailoverCount(trace)) {
		return false
	}
	if len(query.StatusCodes) > 0 && !containsTraceInt(query.StatusCodes, trace.StatusCode) {
		return false
	}
	return true
}

func traceMatchesTimeRange(trace RequestTrace, query TraceQuery) bool {
	if !query.StartedFrom.IsZero() && trace.StartedAt.Before(query.StartedFrom) {
		return false
	}
	if !query.StartedTo.IsZero() && trace.StartedAt.After(query.StartedTo) {
		return false
	}
	return true
}

func containsTraceAlias(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsTraceInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func traceFailoverCount(trace RequestTrace) int {
	if trace.AttemptCount > 0 {
		if trace.AttemptCount <= 1 {
			return 0
		}
		return trace.AttemptCount - 1
	}
	if len(trace.Attempts) <= 1 {
		return 0
	}
	return len(trace.Attempts) - 1
}

func collectAvailableAliases(traces []RequestTrace) []string {
	if len(traces) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(traces))
	for _, trace := range traces {
		alias := strings.TrimSpace(trace.Alias)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func collectTraceStats(traces []RequestTrace) TraceStats {
	stats := TraceStats{}
	for _, trace := range traces {
		if trace.Success {
			stats.Success++
		} else {
			stats.Failed++
		}
		if trace.Failover || traceFailoverCount(trace) > 0 {
			stats.Failover++
		}
	}
	return stats
}

func collectAvailableFailoverCounts(traces []RequestTrace) []int {
	if len(traces) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(traces))
	for _, trace := range traces {
		count := traceFailoverCount(trace)
		if _, ok := seen[count]; ok {
			continue
		}
		seen[count] = struct{}{}
		out = append(out, count)
	}
	sort.Ints(out)
	return out
}

func collectAvailableStatusCodes(traces []RequestTrace) []int {
	if len(traces) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(traces))
	for _, trace := range traces {
		if trace.StatusCode <= 0 {
			continue
		}
		if _, ok := seen[trace.StatusCode]; ok {
			continue
		}
		seen[trace.StatusCode] = struct{}{}
		out = append(out, trace.StatusCode)
	}
	sort.Ints(out)
	return out
}
