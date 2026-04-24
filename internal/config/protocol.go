package config

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	ProtocolOpenAIResponses   = "openai-responses"
	ProtocolAnthropicMessages = "anthropic-messages"
	ProtocolOpenAICompatible  = "openai-compatible"
)

func DefaultProviderProtocol() string {
	return ProtocolOpenAIResponses
}

func DefaultAliasProtocol() string {
	return ProtocolOpenAIResponses
}

func NormalizeProviderProtocol(protocol string) string {
	return normalizeProtocol(protocol, DefaultProviderProtocol())
}

func NormalizeAliasProtocol(protocol string) string {
	return normalizeProtocol(protocol, DefaultAliasProtocol())
}

func normalizeProtocol(protocol string, fallback string) string {
	if protocol == "" {
		return fallback
	}
	switch protocol {
	case ProtocolOpenAIResponses:
		return protocol
	case ProtocolAnthropicMessages:
		return protocol
	case ProtocolOpenAICompatible:
		return protocol
	default:
		return protocol
	}
}

func ValidateProtocol(protocol string) error {
	switch protocol {
	case ProtocolOpenAIResponses:
		return nil
	case ProtocolAnthropicMessages:
		return nil
	case ProtocolOpenAICompatible:
		return nil
	case "":
		return fmt.Errorf("missing protocol")
	default:
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func ProtocolsMatch(left string, right string) bool {
	return NormalizeAliasProtocol(left) == NormalizeProviderProtocol(right)
}

func ProtocolDisplayName(protocol string) string {
	switch protocol {
	case ProtocolOpenAIResponses:
		return "OpenAI Responses"
	case ProtocolAnthropicMessages:
		return "Anthropic Messages"
	case ProtocolOpenAICompatible:
		return "OpenAI Compatible"
	default:
		return protocol
	}
}

func ProtocolLocalBasePath(protocol string) string {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolOpenAIResponses:
		return "/v1"
	default:
		return "/v1"
	}
}

func ProtocolLocalRequestPath(protocol string) string {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolOpenAIResponses:
		return ProtocolLocalBasePath(protocol) + "/responses"
	case ProtocolAnthropicMessages:
		return ProtocolLocalBasePath(protocol) + "/messages"
	case ProtocolOpenAICompatible:
		return ProtocolLocalBasePath(protocol) + "/chat/completions"
	default:
		return ProtocolLocalBasePath(protocol) + "/responses"
	}
}

func ProtocolUpstreamRequestPath(protocol string) string {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolOpenAIResponses:
		return "/responses"
	case ProtocolAnthropicMessages:
		return "/messages"
	case ProtocolOpenAICompatible:
		return "/chat/completions"
	default:
		return "/responses"
	}
}

func ProtocolLocalModelsPath(protocol string) string {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolOpenAIResponses:
		return ProtocolLocalBasePath(protocol) + "/models"
	default:
		return ProtocolLocalBasePath(protocol) + "/models"
	}
}

func ProtocolUpstreamModelsPath(protocol string) string {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolOpenAIResponses:
		return "/models"
	default:
		return "/models"
	}
}

func ApplyProtocolAuthHeaders(header http.Header, protocol string, apiKey string) {
	if strings.TrimSpace(apiKey) == "" {
		return
	}
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolAnthropicMessages:
		header.Set("X-Api-Key", apiKey)
	case ProtocolOpenAICompatible:
		header.Set("Authorization", "Bearer "+apiKey)
	default:
		header.Set("Authorization", "Bearer "+apiKey)
	}
}

func ApplyProtocolDefaultHeaders(header http.Header, protocol string) {
	switch NormalizeProviderProtocol(protocol) {
	case ProtocolAnthropicMessages:
		if strings.TrimSpace(header.Get("Anthropic-Version")) == "" {
			header.Set("Anthropic-Version", "2023-06-01")
		}
	}
}
