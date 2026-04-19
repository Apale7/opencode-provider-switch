package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
)

type Service struct {
	configPath string

	mu          sync.Mutex
	proxyCancel context.CancelFunc
	proxyDone   chan struct{}
	proxyErr    error
	proxyStatus ProxyStatusView
}

func NewService(configPath string) *Service {
	return &Service{configPath: strings.TrimSpace(configPath)}
}

func (s *Service) ConfigPath() string {
	if s.configPath != "" {
		return s.configPath
	}
	return config.DefaultPath()
}

func (s *Service) GetOverview(ctx context.Context) (Overview, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return Overview{}, err
	}
	aliases := cfg.AvailableAliasNames()
	sort.Strings(aliases)
	status := s.currentProxyStatus(proxyBindAddress(cfg))
	return Overview{
		ConfigPath:       cfg.Path(),
		ProviderCount:    len(cfg.Providers),
		AliasCount:       len(cfg.Aliases),
		AvailableAliases: aliases,
		Proxy:            status,
		Desktop:          desktopPrefsView(cfg.Desktop),
	}, nil
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	views := make([]ProviderView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, providerView(provider))
	}
	return views, nil
}

func (s *Service) ListAliases(ctx context.Context) ([]AliasView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	aliases := append([]config.Alias(nil), cfg.Aliases...)
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
	views := make([]AliasView, 0, len(aliases))
	for _, alias := range aliases {
		views = append(views, aliasView(cfg, alias))
	}
	return views, nil
}

func (s *Service) RunDoctor(ctx context.Context) (DoctorReport, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return DoctorReport{}, err
	}
	issues := cfg.Validate()
	path, existed := opencode.ResolveGlobalConfigPath()
	raw, err := opencode.Load(path)
	if err != nil {
		issues = append(issues, fmt.Errorf("load opencode config target: %w", err))
	} else {
		aliasNames := cfg.AvailableAliasNames()
		baseURL := proxyBaseURL(cfg)
		opencode.EnsureOcswitchProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
		if err := opencode.ValidateOcswitchProvider(raw, baseURL, cfg.Server.APIKey, aliasNames); err != nil {
			issues = append(issues, fmt.Errorf("opencode provider.ocswitch invalid: %w", err))
		}
	}
	report := DoctorReport{
		OK:                  len(issues) == 0,
		Issues:              doctorIssues(issues),
		ConfigPath:          cfg.Path(),
		ProviderCount:       len(cfg.Providers),
		AliasCount:          len(cfg.Aliases),
		ProxyBindAddress:    proxyBindAddress(cfg),
		OpenCodeTargetPath:  path,
		OpenCodeTargetFound: existed,
	}
	if report.OK {
		return report, nil
	}
	return report, fmt.Errorf("%d config issue(s)", len(issues))
}

func (s *Service) PreviewOpenCodeSync(ctx context.Context, in SyncInput) (SyncPreview, error) {
	prepared, err := s.prepareSync(ctx, in)
	if err != nil {
		return SyncPreview{}, err
	}
	return SyncPreview{
		TargetPath:    prepared.targetPath,
		AliasNames:    append([]string(nil), prepared.aliasNames...),
		SetModel:      in.SetModel,
		SetSmallModel: in.SetSmallModel,
		WouldChange:   prepared.changed,
	}, nil
}

func (s *Service) ApplyOpenCodeSync(ctx context.Context, in SyncInput) (SyncResult, error) {
	prepared, err := s.prepareSync(ctx, in)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{
		TargetPath:    prepared.targetPath,
		AliasNames:    append([]string(nil), prepared.aliasNames...),
		Changed:       prepared.changed,
		DryRun:        in.DryRun,
		SetModel:      in.SetModel,
		SetSmallModel: in.SetSmallModel,
	}
	if !prepared.changed || in.DryRun {
		return result, nil
	}
	if err := opencode.Save(prepared.targetPath, prepared.raw); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

func (s *Service) StartProxy(ctx context.Context) error {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return errs[0]
	}

	bindAddress := proxyBindAddress(cfg)
	started := false

	s.mu.Lock()
	if s.proxyCancel == nil {
		runCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		s.proxyCancel = cancel
		s.proxyDone = done
		s.proxyErr = nil
		s.proxyStatus = ProxyStatusView{
			Running:     true,
			BindAddress: bindAddress,
			StartedAt:   time.Now(),
		}
		started = true
		go s.runProxy(runCtx, cancel, done, cfg, bindAddress)
	}
	s.mu.Unlock()

	if !started {
		return nil
	}
	return nil
}

func (s *Service) StopProxy(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.proxyCancel
	done := s.proxyDone
	s.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) WaitProxy(ctx context.Context) error {
	s.mu.Lock()
	done := s.proxyDone
	proxyErr := s.proxyErr
	s.mu.Unlock()
	if done == nil {
		return proxyErr
	}
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.proxyErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) GetProxyStatus(ctx context.Context) (ProxyStatusView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ProxyStatusView{}, err
	}
	return s.currentProxyStatus(proxyBindAddress(cfg)), nil
}

func (s *Service) GetDesktopPrefs(ctx context.Context) (DesktopPrefsView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return DesktopPrefsView{}, err
	}
	return desktopPrefsView(cfg.Desktop), nil
}

func (s *Service) SaveDesktopPrefs(ctx context.Context, in DesktopPrefsInput) (DesktopPrefsView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return DesktopPrefsView{}, err
	}
	cfg.Desktop = config.Desktop{
		LaunchAtLogin:  in.LaunchAtLogin,
		MinimizeToTray: in.MinimizeToTray,
		Notifications:  in.Notifications,
		Theme:          normalizeThemePreference(in.Theme),
		Language:       normalizeLanguagePreference(in.Language),
	}
	if err := cfg.Save(); err != nil {
		return DesktopPrefsView{}, err
	}
	return desktopPrefsView(cfg.Desktop), nil
}

type preparedSync struct {
	targetPath string
	aliasNames []string
	raw        opencode.Raw
	changed    bool
}

func (s *Service) prepareSync(ctx context.Context, in SyncInput) (preparedSync, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return preparedSync{}, err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return preparedSync{}, errs[0]
	}
	targetPath := strings.TrimSpace(in.Target)
	if targetPath == "" {
		resolved, _ := opencode.ResolveGlobalConfigPath()
		targetPath = resolved
	}
	raw, err := opencode.Load(targetPath)
	if err != nil {
		return preparedSync{}, err
	}
	aliasNames := cfg.AvailableAliasNames()
	sort.Strings(aliasNames)
	if err := validateSyncedModelSelection(in.SetModel, aliasNames, "--set-model"); err != nil {
		return preparedSync{}, err
	}
	if err := validateSyncedModelSelection(in.SetSmallModel, aliasNames, "--set-small-model"); err != nil {
		return preparedSync{}, err
	}
	baseURL := proxyBaseURL(cfg)
	changed := opencode.EnsureOcswitchProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
	if in.SetModel != "" && raw["model"] != in.SetModel {
		raw["model"] = in.SetModel
		changed = true
	}
	if in.SetSmallModel != "" && raw["small_model"] != in.SetSmallModel {
		raw["small_model"] = in.SetSmallModel
		changed = true
	}
	return preparedSync{targetPath: targetPath, aliasNames: aliasNames, raw: raw, changed: changed}, nil
}

func (s *Service) loadConfig() (*config.Config, error) {
	return config.Load(s.configPath)
}

func (s *Service) currentProxyStatus(bindAddress string) ProxyStatusView {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.proxyStatus
	if !status.Running || status.BindAddress == "" {
		status.BindAddress = bindAddress
	}
	return status
}

func (s *Service) runProxy(runCtx context.Context, cancel context.CancelFunc, done chan struct{}, cfg *config.Config, bindAddress string) {
	err := proxy.New(cfg).ListenAndServe(runCtx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxyDone == done {
		s.proxyCancel = nil
		s.proxyDone = nil
	}
	s.proxyErr = err
	status := s.proxyStatus
	status.Running = false
	status.BindAddress = bindAddress
	if err != nil {
		status.LastError = err.Error()
	} else {
		status.LastError = ""
	}
	s.proxyStatus = status
	close(done)
}

func providerView(provider config.Provider) ProviderView {
	return ProviderView{
		ID:           provider.ID,
		Name:         provider.Name,
		BaseURL:      provider.BaseURL,
		APIKeySet:    provider.APIKey != "",
		APIKeyMasked: maskKey(provider.APIKey),
		Headers:      cloneHeaders(provider.Headers),
		Models:       append([]string(nil), provider.Models...),
		ModelsSource: provider.ModelsSource,
		Disabled:     provider.Disabled,
	}
}

func aliasView(cfg *config.Config, alias config.Alias) AliasView {
	targets := make([]AliasTargetView, 0, len(alias.Targets))
	for _, target := range alias.Targets {
		targets = append(targets, AliasTargetView{
			Provider: target.Provider,
			Model:    target.Model,
			Enabled:  target.Enabled,
		})
	}
	return AliasView{
		Alias:                alias.Alias,
		DisplayName:          alias.DisplayName,
		Enabled:              alias.Enabled,
		TargetCount:          len(alias.Targets),
		AvailableTargetCount: len(cfg.AvailableTargets(alias)),
		Targets:              targets,
	}
}

func doctorIssues(errs []error) []DoctorIssue {
	issues := make([]DoctorIssue, 0, len(errs))
	for _, err := range errs {
		issues = append(issues, DoctorIssue{Message: err.Error()})
	}
	return issues
}

func desktopPrefsView(prefs config.Desktop) DesktopPrefsView {
	return DesktopPrefsView{
		LaunchAtLogin:  prefs.LaunchAtLogin,
		MinimizeToTray: prefs.MinimizeToTray,
		Notifications:  prefs.Notifications,
		Theme:          normalizeThemePreference(prefs.Theme),
		Language:       normalizeLanguagePreference(prefs.Language),
	}
}

func normalizeThemePreference(value string) string {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "light", "dark":
		return trimmed
	default:
		return "system"
	}
}

func normalizeLanguagePreference(value string) string {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "en-US", "zh-CN":
		return trimmed
	default:
		return "system"
	}
}

func validateSyncedModelSelection(value string, aliases []string, flagName string) error {
	if value == "" {
		return nil
	}
	const prefix = "ocswitch/"
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("%s must use the ocswitch/<alias> form", flagName)
	}
	alias := strings.TrimPrefix(value, prefix)
	if alias == "" {
		return fmt.Errorf("%s must use the ocswitch/<alias> form", flagName)
	}
	for _, name := range aliases {
		if name == alias {
			return nil
		}
	}
	if len(aliases) == 0 {
		return fmt.Errorf("%s requires at least one routable alias; run ocswitch alias list or doctor first", flagName)
	}
	choices := make([]string, 0, len(aliases))
	for _, name := range aliases {
		choices = append(choices, prefix+name)
	}
	return fmt.Errorf("%s %q is not a routable alias; available: %s", flagName, value, strings.Join(choices, ", "))
}

func proxyBindAddress(cfg *config.Config) string {
	return fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
}

func proxyBaseURL(cfg *config.Config) string {
	return fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "…" + k[len(k)-4:]
}
