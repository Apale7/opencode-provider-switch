package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

const ssePrecommitBufferCapBytes = 256 << 10

type sseStreamSignal struct {
	commitWorth bool
	terminal    bool
}

type sseStreamState struct {
	protocol string
	pending  bytes.Buffer
}

type sseFrame struct {
	event string
	data  string
	lines int
}

func newSSEStreamState(protocol string) *sseStreamState {
	return &sseStreamState{protocol: config.NormalizeProviderProtocol(protocol)}
}

func (s *sseStreamState) Add(chunk []byte) sseStreamSignal {
	if s == nil || len(chunk) == 0 {
		return sseStreamSignal{}
	}
	_, _ = s.pending.Write(chunk)
	var signal sseStreamSignal
	for {
		rawFrame, ok := nextSSEFrame(&s.pending)
		if !ok {
			return signal
		}
		frame := parseSSEFrame(rawFrame)
		frameSignal := classifySSEFrame(s.protocol, frame)
		signal.commitWorth = signal.commitWorth || frameSignal.commitWorth
		signal.terminal = signal.terminal || frameSignal.terminal
	}
}

func parseSSEFrame(raw string) sseFrame {
	frame := sseFrame{}
	dataLines := make([]string, 0, 1)
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if line == "" {
			continue
		}
		frame.lines++
		switch {
		case strings.HasPrefix(line, ":"):
			continue
		case strings.HasPrefix(line, "event:"):
			frame.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	frame.data = strings.Join(dataLines, "\n")
	return frame
}

func classifySSEFrame(protocol string, frame sseFrame) sseStreamSignal {
	if frame.lines == 0 {
		return sseStreamSignal{}
	}
	switch config.NormalizeProviderProtocol(protocol) {
	case config.ProtocolAnthropicMessages:
		return classifyAnthropicSSEFrame(frame)
	case config.ProtocolOpenAICompatible:
		return classifyOpenAICompatibleSSEFrame(frame)
	default:
		return classifyOpenAIResponsesSSEFrame(frame)
	}
}

func classifyOpenAIResponsesSSEFrame(frame sseFrame) sseStreamSignal {
	data := strings.TrimSpace(frame.data)
	if data == "" || data == "[DONE]" {
		return sseStreamSignal{}
	}
	payload, ok := parseSSEJSONData(data)
	typeName := payloadType(payload)
	if frame.event == "response.completed" {
		return sseStreamSignal{terminal: ok}
	}
	if !ok {
		return sseStreamSignal{commitWorth: true}
	}
	if typeName == "response.completed" {
		return sseStreamSignal{terminal: true}
	}
	if typeName == "response.incomplete" {
		return sseStreamSignal{commitWorth: true}
	}
	if frame.event == "response.created" || typeName == "response.created" || frame.event == "ping" {
		return sseStreamSignal{}
	}
	if signal, known := classifyOpenAIResponsesOutputFrame(frame.event, payload); known {
		return signal
	}
	if signal, known := classifyOpenAIResponsesOutputFrame(typeName, payload); known {
		return signal
	}
	return sseStreamSignal{commitWorth: true}
}

func classifyOpenAIResponsesOutputFrame(eventName string, payload map[string]any) (sseStreamSignal, bool) {
	switch eventName {
	case "response.output_text.delta", "response.function_call_arguments.delta":
		return sseStreamSignal{commitWorth: mapHasNonEmptyString(payload, "delta", "text", "arguments")}, true
	case "response.output_item.added", "response.output_item.done":
		return sseStreamSignal{commitWorth: true}, true
	default:
		return sseStreamSignal{}, false
	}
}

func classifyOpenAICompatibleSSEFrame(frame sseFrame) sseStreamSignal {
	data := strings.TrimSpace(frame.data)
	if data == "[DONE]" {
		return sseStreamSignal{terminal: true}
	}
	if data == "" {
		return sseStreamSignal{}
	}
	payload, ok := parseSSEJSONData(data)
	if !ok {
		return sseStreamSignal{commitWorth: true}
	}
	if openAICompatibleChunkHasOutput(payload) {
		return sseStreamSignal{commitWorth: true}
	}
	if openAICompatibleChunkIsMetadataOnly(payload) {
		return sseStreamSignal{}
	}
	return sseStreamSignal{commitWorth: true}
}

func classifyAnthropicSSEFrame(frame sseFrame) sseStreamSignal {
	data := strings.TrimSpace(frame.data)
	if frame.event == "message_stop" {
		return sseStreamSignal{terminal: true}
	}
	if data == "" {
		return sseStreamSignal{}
	}
	payload, ok := parseSSEJSONData(data)
	typeName := ""
	if ok {
		typeName = payloadType(payload)
	}
	if typeName == "message_stop" {
		return sseStreamSignal{terminal: true}
	}
	if frame.event == "message_start" || frame.event == "ping" || typeName == "message_start" || typeName == "ping" {
		return sseStreamSignal{}
	}
	if frame.event == "content_block_start" {
		return sseStreamSignal{commitWorth: true}
	}
	if frame.event == "content_block_delta" || frame.event == "input_json_delta" {
		return sseStreamSignal{commitWorth: !ok || anthropicContentBlockHasOutput(payload)}
	}
	if frame.event == "message_delta" {
		if ok && anthropicMessageDeltaHasOutput(payload) {
			return sseStreamSignal{commitWorth: true}
		}
		return sseStreamSignal{}
	}
	if typeName == "content_block_start" {
		return sseStreamSignal{commitWorth: true}
	}
	if typeName == "content_block_delta" || typeName == "input_json_delta" {
		return sseStreamSignal{commitWorth: anthropicContentBlockHasOutput(payload)}
	}
	if !ok {
		return sseStreamSignal{commitWorth: true}
	}
	return sseStreamSignal{commitWorth: true}
}

func parseSSEJSONData(data string) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func payloadType(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	typeName, _ := payload["type"].(string)
	return typeName
}

func openAICompatibleChunkHasOutput(payload map[string]any) bool {
	for _, choice := range openAICompatibleChoices(payload) {
		delta, _ := choice["delta"].(map[string]any)
		if len(delta) == 0 {
			continue
		}
		if content, ok := delta["content"].(string); ok && content != "" {
			return true
		}
		if toolCalls, ok := delta["tool_calls"].([]any); ok && openAICompatibleToolCallsHaveOutput(toolCalls) {
			return true
		}
		if functionCall, ok := delta["function_call"].(map[string]any); ok && mapHasNonEmptyString(functionCall, "arguments") {
			return true
		}
	}
	return false
}

func openAICompatibleChunkIsMetadataOnly(payload map[string]any) bool {
	choices := openAICompatibleChoices(payload)
	if len(choices) == 0 {
		return false
	}
	for _, choice := range choices {
		delta, _ := choice["delta"].(map[string]any)
		for key, value := range delta {
			switch key {
			case "role", "content", "tool_calls", "function_call":
				if key == "content" {
					if text, ok := value.(string); ok && text != "" {
						return false
					}
				}
				if key == "tool_calls" {
					toolCalls, _ := value.([]any)
					if openAICompatibleToolCallsHaveOutput(toolCalls) {
						return false
					}
				}
				if key == "function_call" {
					functionCall, _ := value.(map[string]any)
					if mapHasNonEmptyString(functionCall, "arguments") {
						return false
					}
				}
			default:
				return false
			}
		}
	}
	return true
}

func openAICompatibleChoices(payload map[string]any) []map[string]any {
	choicesRaw, _ := payload["choices"].([]any)
	choices := make([]map[string]any, 0, len(choicesRaw))
	for _, item := range choicesRaw {
		choice, ok := item.(map[string]any)
		if ok {
			choices = append(choices, choice)
		}
	}
	return choices
}

func openAICompatibleToolCallsHaveOutput(toolCalls []any) bool {
	for _, item := range toolCalls {
		toolCall, ok := item.(map[string]any)
		if !ok {
			continue
		}
		function, _ := toolCall["function"].(map[string]any)
		if mapHasNonEmptyString(function, "arguments") {
			return true
		}
	}
	return false
}

func openAIResponsesItemHasOutput(payload map[string]any) bool {
	if mapHasNonEmptyString(payload, "delta", "text", "arguments", "output_text") {
		return true
	}
	if content, ok := payload["content"].([]any); ok && sseContentArrayHasOutput(content) {
		return true
	}
	item, _ := payload["item"].(map[string]any)
	if len(item) == 0 {
		return false
	}
	if mapHasNonEmptyString(item, "text", "output_text", "arguments") {
		return true
	}
	if content, ok := item["content"].([]any); ok && sseContentArrayHasOutput(content) {
		return true
	}
	itemType, _ := item["type"].(string)
	if strings.Contains(itemType, "function_call") || strings.Contains(itemType, "tool_call") {
		return nonEmptyJSONObject(item["input"])
	}
	return false
}

func sseContentArrayHasOutput(content []any) bool {
	for _, item := range content {
		contentItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if mapHasNonEmptyString(contentItem, "text", "output_text", "refusal", "partial_json") {
			return true
		}
		if nonEmptyJSONObject(contentItem["input"]) {
			return true
		}
	}
	return false
}

func anthropicMessageDeltaHasOutput(payload map[string]any) bool {
	delta, _ := payload["delta"].(map[string]any)
	for key, value := range delta {
		switch key {
		case "text", "partial_json", "thinking", "signature":
			if text, ok := value.(string); ok && text != "" {
				return true
			}
		case "tool_use":
			return true
		}
	}
	return false
}

func anthropicContentBlockHasOutput(payload map[string]any) bool {
	if anthropicMessageDeltaHasOutput(payload) {
		return true
	}
	if mapHasNonEmptyString(payload, "text", "partial_json", "thinking", "signature") {
		return true
	}
	if nonEmptyJSONObject(payload["input"]) {
		return true
	}
	contentBlock, _ := payload["content_block"].(map[string]any)
	if len(contentBlock) == 0 {
		return false
	}
	if mapHasNonEmptyString(contentBlock, "text", "partial_json", "thinking", "signature") {
		return true
	}
	if nonEmptyJSONObject(contentBlock["input"]) {
		return true
	}
	blockType, _ := contentBlock["type"].(string)
	if blockType == "tool_use" {
		return false
	}
	return false
}

func mapHasNonEmptyString(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := payload[key].(string)
		if ok && value != "" {
			return true
		}
	}
	return false
}

func nonEmptyJSONObject(value any) bool {
	object, ok := value.(map[string]any)
	return ok && len(object) > 0
}

func sseStreamErrorEvent(protocol string, code string, message string) []byte {
	protocol = config.NormalizeProviderProtocol(protocol)
	switch protocol {
	case config.ProtocolAnthropicMessages:
		payload, _ := json.Marshal(anthropicErrorEnvelope{
			Type: "error",
			Error: anthropicError{
				Type:    "api_error",
				Message: message,
			},
		})
		return []byte(fmt.Sprintf("event: error\ndata: %s\n\n", payload))
	case config.ProtocolOpenAIResponses:
		payload, _ := json.Marshal(openAIResponsesSSEErrorEnvelope{
			Type: "error",
			Error: openAIError{
				Message: message,
				Type:    "server_error",
				Code:    code,
			},
		})
		return []byte(fmt.Sprintf("event: error\ndata: %s\n\n", payload))
	default:
		payload, _ := json.Marshal(openAIErrorEnvelope{
			Error: openAIError{
				Message: message,
				Type:    "server_error",
				Code:    code,
			},
		})
		return []byte(fmt.Sprintf("data: %s\n\n", payload))
	}
}

type openAIResponsesSSEErrorEnvelope struct {
	Type  string      `json:"type"`
	Error openAIError `json:"error"`
}
