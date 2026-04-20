package config

import "fmt"

const (
	ProtocolOpenAIResponses = "openai-responses"
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
	default:
		return protocol
	}
}

func ValidateProtocol(protocol string) error {
	switch protocol {
	case ProtocolOpenAIResponses:
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
	default:
		return ProtocolLocalBasePath(protocol) + "/responses"
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
