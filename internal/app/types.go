package app

import "time"

type Overview struct {
	ConfigPath       string           `json:"configPath"`
	ProviderCount    int              `json:"providerCount"`
	AliasCount       int              `json:"aliasCount"`
	AvailableAliases []string         `json:"availableAliases"`
	Proxy            ProxyStatusView  `json:"proxy"`
	Desktop          DesktopPrefsView `json:"desktop"`
}

type ProxyStatusView struct {
	Running     bool      `json:"running"`
	BindAddress string    `json:"bindAddress"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
}

type ProviderView struct {
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	BaseURL      string            `json:"baseUrl"`
	APIKeySet    bool              `json:"apiKeySet"`
	APIKeyMasked string            `json:"apiKeyMasked,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Models       []string          `json:"models,omitempty"`
	ModelsSource string            `json:"modelsSource,omitempty"`
	Disabled     bool              `json:"disabled"`
}

type ProviderSaveResult struct {
	Provider ProviderView `json:"provider"`
	Warnings []string     `json:"warnings,omitempty"`
}

type AliasTargetView struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
}

type AliasView struct {
	Alias                string            `json:"alias"`
	DisplayName          string            `json:"displayName,omitempty"`
	Enabled              bool              `json:"enabled"`
	TargetCount          int               `json:"targetCount"`
	AvailableTargetCount int               `json:"availableTargetCount"`
	Targets              []AliasTargetView `json:"targets"`
}

type DoctorIssue struct {
	Message string `json:"message"`
}

type DoctorReport struct {
	OK                  bool          `json:"ok"`
	Issues              []DoctorIssue `json:"issues"`
	ConfigPath          string        `json:"configPath"`
	ProviderCount       int           `json:"providerCount"`
	AliasCount          int           `json:"aliasCount"`
	ProxyBindAddress    string        `json:"proxyBindAddress"`
	OpenCodeTargetPath  string        `json:"openCodeTargetPath"`
	OpenCodeTargetFound bool          `json:"openCodeTargetFound"`
}

type DoctorRunResult struct {
	Report DoctorReport `json:"report"`
	Error  string       `json:"error,omitempty"`
}

type SyncInput struct {
	Target        string `json:"target,omitempty"`
	SetModel      string `json:"setModel,omitempty"`
	SetSmallModel string `json:"setSmallModel,omitempty"`
	DryRun        bool   `json:"dryRun"`
}

type SyncPreview struct {
	TargetPath    string   `json:"targetPath"`
	AliasNames    []string `json:"aliasNames"`
	SetModel      string   `json:"setModel,omitempty"`
	SetSmallModel string   `json:"setSmallModel,omitempty"`
	WouldChange   bool     `json:"wouldChange"`
}

type SyncResult struct {
	TargetPath    string   `json:"targetPath"`
	AliasNames    []string `json:"aliasNames"`
	Changed       bool     `json:"changed"`
	DryRun        bool     `json:"dryRun"`
	SetModel      string   `json:"setModel,omitempty"`
	SetSmallModel string   `json:"setSmallModel,omitempty"`
}

type DesktopPrefsView struct {
	LaunchAtLogin  bool `json:"launchAtLogin"`
	MinimizeToTray bool `json:"minimizeToTray"`
	Notifications  bool `json:"notifications"`
}

type DesktopPrefsSaveResult struct {
	Prefs    DesktopPrefsView `json:"prefs"`
	Warnings []string         `json:"warnings,omitempty"`
}

type DesktopPrefsInput struct {
	LaunchAtLogin  bool `json:"launchAtLogin"`
	MinimizeToTray bool `json:"minimizeToTray"`
	Notifications  bool `json:"notifications"`
}

type ProviderUpsertInput struct {
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	BaseURL      string            `json:"baseUrl"`
	APIKey       string            `json:"apiKey,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Disabled     bool              `json:"disabled"`
	SkipModels   bool              `json:"skipModels"`
	ClearHeaders bool              `json:"clearHeaders"`
}

type ProviderImportInput struct {
	SourcePath string `json:"sourcePath,omitempty"`
	Overwrite  bool   `json:"overwrite"`
}

type ProviderImportResult struct {
	SourcePath string   `json:"sourcePath"`
	Imported   int      `json:"imported"`
	Skipped    int      `json:"skipped"`
	Warnings   []string `json:"warnings,omitempty"`
}

type ProviderStateInput struct {
	ID       string `json:"id"`
	Disabled bool   `json:"disabled"`
}

type AliasUpsertInput struct {
	Alias       string `json:"alias"`
	DisplayName string `json:"displayName,omitempty"`
	Disabled    bool   `json:"disabled"`
}

type AliasTargetInput struct {
	Alias    string `json:"alias"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
}
