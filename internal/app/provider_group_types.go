package app

// ProviderGroupInput is the management write contract for a provider group.
// It is intentionally separate from client proxy API key DTOs.
type ProviderGroupInput struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	NameChanged    bool     `json:"nameChanged,omitempty"`
	Protocol       string   `json:"protocol"`
	APIKeysChanged bool     `json:"apiKeysChanged"`
	APIKeys        []string `json:"apiKeys,omitempty"`
	Models         []string `json:"models,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
}

// ProviderGroupView is the management read contract for a provider group.
// Responses expose only masked keys and never echo plaintext secrets.
type ProviderGroupView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Protocol      string   `json:"protocol"`
	APIKeyCount   int      `json:"apiKeyCount"`
	APIKeysMasked []string `json:"apiKeysMasked,omitempty"`
	Models        []string `json:"models,omitempty"`
	ModelsSource  string   `json:"modelsSource,omitempty"`
	Disabled      bool     `json:"disabled"`
}

// ProviderGroupSelectorInput identifies an exact (provider, group) pair for
// management write payloads such as rewrite rules.
type ProviderGroupSelectorInput struct {
	Provider string `json:"provider"`
	Group    string `json:"group"`
}

// ProviderGroupSelectorView identifies an exact (provider, group) pair in
// management read payloads such as rewrite rule views.
type ProviderGroupSelectorView struct {
	Provider string `json:"provider"`
	Group    string `json:"group"`
}
