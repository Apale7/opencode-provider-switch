package proxy

import (
	"bytes"
	"encoding/json"
	"mime"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

const maxJSONUsageCaptureBytes = 256 << 10

type tokenUsage struct {
	rawInputTokens     *int64
	rawOutputTokens    *int64
	rawTotalTokens     *int64
	inputTokens        *int64
	outputTokens       *int64
	reasoningTokens    *int64
	cacheReadTokens    *int64
	cacheWriteTokens   *int64
	cacheWrite1HTokens *int64
	source             string
	precision          string
	notes              []string
}

type usageCollector interface {
	Add(chunk []byte)
	Usage() tokenUsage
}

func (u tokenUsage) projectInputTokens() int64 {
	if u.inputTokens == nil {
		return 0
	}
	return *u.inputTokens
}

func (u tokenUsage) projectOutputTokens() int64 {
	if u.outputTokens == nil {
		return 0
	}
	return *u.outputTokens
}

func (u tokenUsage) hasData() bool {
	return u.rawInputTokens != nil ||
		u.rawOutputTokens != nil ||
		u.rawTotalTokens != nil ||
		u.inputTokens != nil ||
		u.outputTokens != nil ||
		u.reasoningTokens != nil ||
		u.cacheReadTokens != nil ||
		u.cacheWriteTokens != nil ||
		u.cacheWrite1HTokens != nil
}

func (u tokenUsage) withNote(note string) tokenUsage {
	note = strings.TrimSpace(note)
	if note == "" {
		return u
	}
	for _, existing := range u.notes {
		if existing == note {
			return u
		}
	}
	u.notes = append(u.notes, note)
	return u
}

func unavailableUsage(source string, note string) tokenUsage {
	usage := tokenUsage{source: source, precision: "unavailable"}
	return usage.withNote(note)
}

func newUsageCollector(protocol string, contentType string) usageCollector {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	protocol = config.NormalizeProviderProtocol(protocol)
	if mediaType == "text/event-stream" {
		switch protocol {
		case config.ProtocolAnthropicMessages:
			return &anthropicSSEUsageCollector{}
		case config.ProtocolOpenAICompatible:
			return &openAICompatibleSSEUsageCollector{}
		default:
			return &openAIResponsesSSEUsageCollector{}
		}
	}
	switch protocol {
	case config.ProtocolAnthropicMessages:
		return &jsonUsageCollector{source: protocol, parse: parseAnthropicJSONUsage}
	case config.ProtocolOpenAICompatible:
		return &jsonUsageCollector{source: protocol, parse: parseOpenAICompatibleJSONUsage}
	default:
		return &jsonUsageCollector{source: protocol, parse: parseOpenAIResponsesJSONUsage}
	}
}

type jsonUsageCollector struct {
	buf       bytes.Buffer
	truncated bool
	source    string
	parse     func(any) (tokenUsage, bool)
}

func (c *jsonUsageCollector) Add(chunk []byte) {
	if c == nil || c.truncated || len(chunk) == 0 {
		return
	}
	remaining := maxJSONUsageCaptureBytes - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return
	}
	if len(chunk) > remaining {
		chunk = chunk[:remaining]
		c.truncated = true
	}
	_, _ = c.buf.Write(chunk)
}

func (c *jsonUsageCollector) Usage() tokenUsage {
	if c == nil {
		return tokenUsage{}
	}
	if c.truncated {
		return unavailableUsage(c.source, "response body exceeded usage capture limit")
	}
	if c.buf.Len() == 0 {
		return unavailableUsage(c.source, "upstream response body was empty")
	}
	if c.parse == nil {
		return unavailableUsage(c.source, "usage parser unavailable")
	}
	var payload any
	if err := json.Unmarshal(c.buf.Bytes(), &payload); err != nil {
		return unavailableUsage(c.source, "failed to parse usage from response body")
	}
	usage, ok := c.parse(payload)
	if !ok {
		return unavailableUsage(c.source, "upstream response did not report usage")
	}
	return usage
}

type openAIResponsesSSEUsageCollector struct {
	pending    bytes.Buffer
	finalUsage *tokenUsage
}

func (c *openAIResponsesSSEUsageCollector) Add(chunk []byte) {
	if c == nil || len(chunk) == 0 {
		return
	}
	_, _ = c.pending.Write(chunk)
	for {
		frame, ok := nextSSEFrame(&c.pending)
		if !ok {
			return
		}
		c.consumeFrame(frame)
	}
}

func (c *openAIResponsesSSEUsageCollector) Usage() tokenUsage {
	if c == nil {
		return tokenUsage{}
	}
	c.flushPending()
	if c.finalUsage == nil {
		return unavailableUsage("openai-responses", "upstream stream ended before final usage event")
	}
	return *c.finalUsage
}

func (c *openAIResponsesSSEUsageCollector) flushPending() {
	if c == nil || c.pending.Len() == 0 {
		return
	}
	frame := strings.TrimSpace(c.pending.String())
	c.pending.Reset()
	if frame != "" {
		c.consumeFrame(frame)
	}
	return
}

func (c *openAIResponsesSSEUsageCollector) consumeFrame(frame string) {
	eventName := ""
	dataLines := make([]string, 0, 1)
	for _, rawLine := range strings.Split(frame, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				dataLines = append(dataLines, data)
			}
		}
	}
	if eventName != "response.completed" && eventName != "response.incomplete" {
		return
	}
	data := strings.Join(dataLines, "\n")
	if data == "" || data == "[DONE]" {
		return
	}
	var payload any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return
	}
	usage, ok := parseOpenAIResponsesJSONUsage(payload)
	if !ok {
		return
	}
	c.finalUsage = &usage
}

type openAICompatibleSSEUsageCollector struct {
	pending    bytes.Buffer
	finalUsage *tokenUsage
}

func (c *openAICompatibleSSEUsageCollector) Add(chunk []byte) {
	if c == nil || len(chunk) == 0 {
		return
	}
	_, _ = c.pending.Write(chunk)
	for {
		frame, ok := nextSSEFrame(&c.pending)
		if !ok {
			return
		}
		c.consumeFrame(frame)
	}
}

func (c *openAICompatibleSSEUsageCollector) Usage() tokenUsage {
	if c == nil {
		return tokenUsage{}
	}
	c.flushPending()
	if c.finalUsage == nil {
		return unavailableUsage("openai-compatible", "upstream stream ended before final usage event")
	}
	return *c.finalUsage
}

func (c *openAICompatibleSSEUsageCollector) flushPending() {
	if c == nil || c.pending.Len() == 0 {
		return
	}
	frame := strings.TrimSpace(c.pending.String())
	c.pending.Reset()
	if frame != "" {
		c.consumeFrame(frame)
	}
}

func (c *openAICompatibleSSEUsageCollector) consumeFrame(frame string) {
	dataLines := make([]string, 0, 1)
	for _, rawLine := range strings.Split(frame, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				dataLines = append(dataLines, data)
			}
		}
	}
	data := strings.Join(dataLines, "\n")
	if data == "" || data == "[DONE]" {
		return
	}
	var payload any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return
	}
	usage, ok := parseOpenAICompatibleJSONUsage(payload)
	if !ok {
		return
	}
	c.finalUsage = &usage
}

type anthropicSSEUsageCollector struct {
	pending bytes.Buffer
	usage   anthropicUsageState
	seen    bool
}

func (c *anthropicSSEUsageCollector) Add(chunk []byte) {
	if c == nil || len(chunk) == 0 {
		return
	}
	_, _ = c.pending.Write(chunk)
	for {
		frame, ok := nextSSEFrame(&c.pending)
		if !ok {
			return
		}
		c.consumeFrame(frame)
	}
}

func (c *anthropicSSEUsageCollector) Usage() tokenUsage {
	if c == nil {
		return tokenUsage{}
	}
	c.flushPending()
	if !c.seen {
		return unavailableUsage("anthropic-messages", "upstream stream did not report usage")
	}
	usage, ok := c.usage.normalize("exact")
	if !ok {
		return unavailableUsage("anthropic-messages", "upstream stream did not report usage")
	}
	return usage
}

func (c *anthropicSSEUsageCollector) flushPending() {
	if c == nil || c.pending.Len() == 0 {
		return
	}
	frame := strings.TrimSpace(c.pending.String())
	c.pending.Reset()
	if frame != "" {
		c.consumeFrame(frame)
	}
}

func (c *anthropicSSEUsageCollector) consumeFrame(frame string) {
	dataLines := make([]string, 0, 1)
	for _, rawLine := range strings.Split(frame, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				dataLines = append(dataLines, data)
			}
		}
	}
	data := strings.Join(dataLines, "\n")
	if data == "" || data == "[DONE]" {
		return
	}
	var payload any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return
	}
	update, ok := parseAnthropicUsageUpdate(payload)
	if !ok {
		return
	}
	c.usage.merge(update)
	c.seen = true
}

type anthropicUsageState struct {
	inputTokens        *int64
	outputTokens       *int64
	cacheReadTokens    *int64
	cacheWriteTokens   *int64
	cacheWrite1HTokens *int64
}

func (s *anthropicUsageState) merge(next anthropicUsageState) {
	if next.inputTokens != nil {
		s.inputTokens = next.inputTokens
	}
	if next.outputTokens != nil {
		s.outputTokens = next.outputTokens
	}
	if next.cacheReadTokens != nil {
		s.cacheReadTokens = next.cacheReadTokens
	}
	if next.cacheWriteTokens != nil {
		s.cacheWriteTokens = next.cacheWriteTokens
	}
	if next.cacheWrite1HTokens != nil {
		s.cacheWrite1HTokens = next.cacheWrite1HTokens
	}
}

func (s anthropicUsageState) normalize(precision string) (tokenUsage, bool) {
	if s.inputTokens == nil && s.outputTokens == nil && s.cacheReadTokens == nil && s.cacheWriteTokens == nil && s.cacheWrite1HTokens == nil {
		return tokenUsage{}, false
	}
	usage := tokenUsage{
		rawInputTokens:     s.inputTokens,
		rawOutputTokens:    s.outputTokens,
		inputTokens:        s.inputTokens,
		outputTokens:       s.outputTokens,
		cacheReadTokens:    s.cacheReadTokens,
		cacheWriteTokens:   s.cacheWriteTokens,
		cacheWrite1HTokens: s.cacheWrite1HTokens,
		source:             "anthropic-messages",
		precision:          precision,
	}
	if s.inputTokens != nil || s.outputTokens != nil {
		total := int64(0)
		if s.inputTokens != nil {
			total += *s.inputTokens
		}
		if s.outputTokens != nil {
			total += *s.outputTokens
		}
		usage.rawTotalTokens = int64Ptr(total)
	}
	return usage, true
}

func parseOpenAIResponsesJSONUsage(payload any) (tokenUsage, bool) {
	root, ok := payload.(map[string]any)
	if !ok {
		return tokenUsage{}, false
	}
	usageValue, ok := root["usage"]
	if !ok {
		response, ok := root["response"].(map[string]any)
		if !ok {
			return tokenUsage{}, false
		}
		usageValue, ok = response["usage"]
		if !ok {
			return tokenUsage{}, false
		}
	}
	usageMap, ok := usageValue.(map[string]any)
	if !ok {
		return tokenUsage{}, false
	}
	rawInput, hasInput := jsonNumberToInt64(usageMap["input_tokens"])
	rawOutput, hasOutput := jsonNumberToInt64(usageMap["output_tokens"])
	if !hasInput && !hasOutput {
		return tokenUsage{}, false
	}
	rawTotal, hasTotal := jsonNumberToInt64(usageMap["total_tokens"])
	cacheRead := nestedInt64(usageMap, "input_tokens_details", "cached_tokens")
	reasoning := nestedInt64(usageMap, "output_tokens_details", "reasoning_tokens")
	input := rawInput - cacheRead
	if input < 0 {
		input = 0
	}
	output := rawOutput - reasoning
	if output < 0 {
		output = 0
	}
	usage := tokenUsage{
		rawInputTokens:  int64Ptr(rawInput),
		rawOutputTokens: int64Ptr(rawOutput),
		inputTokens:     int64Ptr(input),
		outputTokens:    int64Ptr(output),
		reasoningTokens: int64Ptr(reasoning),
		cacheReadTokens: int64Ptr(cacheRead),
		source:          "openai-responses",
		precision:       "exact",
	}
	if hasTotal {
		usage.rawTotalTokens = int64Ptr(rawTotal)
	} else {
		usage.rawTotalTokens = int64Ptr(rawInput + rawOutput)
	}
	return usage, true
}

func parseOpenAICompatibleJSONUsage(payload any) (tokenUsage, bool) {
	root, ok := payload.(map[string]any)
	if !ok {
		return tokenUsage{}, false
	}
	usageValue, ok := root["usage"]
	if !ok {
		return tokenUsage{}, false
	}
	usageMap, ok := usageValue.(map[string]any)
	if !ok {
		return tokenUsage{}, false
	}
	rawInput, hasInput := jsonNumberToInt64(usageMap["prompt_tokens"])
	rawOutput, hasOutput := jsonNumberToInt64(usageMap["completion_tokens"])
	if !hasInput && !hasOutput {
		return tokenUsage{}, false
	}
	rawTotal, hasTotal := jsonNumberToInt64(usageMap["total_tokens"])
	cacheRead := nestedInt64(usageMap, "prompt_tokens_details", "cached_tokens")
	reasoning := nestedInt64(usageMap, "completion_tokens_details", "reasoning_tokens")
	input := rawInput - cacheRead
	if input < 0 {
		input = 0
	}
	output := rawOutput - reasoning
	if output < 0 {
		output = 0
	}
	usage := tokenUsage{
		rawInputTokens:  int64Ptr(rawInput),
		rawOutputTokens: int64Ptr(rawOutput),
		inputTokens:     int64Ptr(input),
		outputTokens:    int64Ptr(output),
		reasoningTokens: int64Ptr(reasoning),
		cacheReadTokens: int64Ptr(cacheRead),
		source:          "openai-compatible",
		precision:       "exact",
	}
	if hasTotal {
		usage.rawTotalTokens = int64Ptr(rawTotal)
	} else {
		usage.rawTotalTokens = int64Ptr(rawInput + rawOutput)
	}
	return usage, true
}

func parseAnthropicJSONUsage(payload any) (tokenUsage, bool) {
	update, ok := parseAnthropicUsageUpdate(payload)
	if !ok {
		return tokenUsage{}, false
	}
	return update.normalize("exact")
}

func parseAnthropicUsageUpdate(payload any) (anthropicUsageState, bool) {
	root, ok := payload.(map[string]any)
	if !ok {
		return anthropicUsageState{}, false
	}
	usageValue, ok := root["usage"]
	if !ok {
		message, ok := root["message"].(map[string]any)
		if !ok {
			return anthropicUsageState{}, false
		}
		usageValue, ok = message["usage"]
		if !ok {
			return anthropicUsageState{}, false
		}
	}
	usageMap, ok := usageValue.(map[string]any)
	if !ok {
		return anthropicUsageState{}, false
	}
	var state anthropicUsageState
	if value, ok := jsonNumberToInt64(usageMap["input_tokens"]); ok {
		state.inputTokens = int64Ptr(value)
	}
	if value, ok := jsonNumberToInt64(usageMap["output_tokens"]); ok {
		state.outputTokens = int64Ptr(value)
	}
	if value, ok := jsonNumberToInt64(usageMap["cache_read_input_tokens"]); ok {
		state.cacheReadTokens = int64Ptr(value)
	}
	if value, ok := nestedInt64OK(usageMap, "cache_creation", "ephemeral_5m_input_tokens"); ok {
		state.cacheWriteTokens = int64Ptr(value)
	} else if value, ok := jsonNumberToInt64(usageMap["cache_creation_input_tokens"]); ok {
		state.cacheWriteTokens = int64Ptr(value)
	}
	if value, ok := nestedInt64OK(usageMap, "cache_creation", "ephemeral_1h_input_tokens"); ok {
		state.cacheWrite1HTokens = int64Ptr(value)
	}
	if state.inputTokens == nil && state.outputTokens == nil && state.cacheReadTokens == nil && state.cacheWriteTokens == nil && state.cacheWrite1HTokens == nil {
		return anthropicUsageState{}, false
	}
	return state, true
}

func nestedInt64OK(value any, path ...string) (int64, bool) {
	current := value
	for _, segment := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return 0, false
		}
		current, ok = obj[segment]
		if !ok {
			return 0, false
		}
	}
	return jsonNumberToInt64(current)
}

func nestedInt64(value any, path ...string) int64 {
	result, _ := nestedInt64OK(value, path...)
	return result
}

func int64Ptr(value int64) *int64 {
	result := value
	return &result
}

func nextSSEFrame(buf *bytes.Buffer) (string, bool) {
	data := buf.Bytes()
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		idx := bytes.Index(data, []byte(sep))
		if idx < 0 {
			continue
		}
		frame := string(data[:idx])
		buf.Next(idx + len(sep))
		return frame, true
	}
	return "", false
}
