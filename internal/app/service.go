package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/configstore"
	"github.com/Apale7/opencode-provider-switch/internal/diagnostics"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
)

type Service struct {
	configPath     string
	traces         proxy.RequestTraceStore
	traceStoreInfo TraceStoreStatus

	mu            sync.Mutex
	proxyCancel   context.CancelFunc
	proxyDone     chan struct{}
	proxyReady    chan struct{}
	proxyReadyErr error
	proxyErr      error
	proxyServer   *proxy.Server
	proxyStatus   ProxyStatusView
}

func NewService(configPath string) *Service {
	resolvedPath := strings.TrimSpace(configPath)
	store, err := proxy.NewSQLiteTraceStore(resolveConfigPath(resolvedPath))
	traceStoreInfo := TraceStoreStatus{Mode: "sqlite"}
	if err != nil {
		traceStoreInfo = TraceStoreStatus{Mode: "memory", Error: err.Error()}
		store = nil
	}
	var traces proxy.RequestTraceStore
	if store != nil {
		traces = store
		traceStoreInfo.Path = store.DBPath()
	} else {
		traces = proxy.NewTraceStore(200)
	}
	return &Service{configPath: resolvedPath, traces: traces, traceStoreInfo: traceStoreInfo}
}

func (s *Service) TraceStoreStatus() TraceStoreStatus {
	if s == nil {
		return TraceStoreStatus{Mode: "memory", Error: "service unavailable"}
	}
	return s.traceStoreInfo
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
		TraceStore:       s.TraceStoreStatus(),
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
	cfg, err := s.loadConfig()
	if err != nil {
		return DoctorReport{}, err
	}
	issues := structuralDoctorIssues(cfg.Validate())
	if typed, scanErr := diagnostics.ScanConfig(cfg, diagnostics.ScanOptions{}); scanErr == nil {
		issues = append(issues, doctorIssuesFromDiagnostics(typed)...)
	} else {
		issues = append(issues, DoctorIssue{
			SchemaVersion: diagnostics.SchemaVersion,
			Code:          string(diagnostics.CodeConfigInvalid),
			Severity:      string(diagnostics.SeverityError),
			Reason:        string(diagnostics.ReasonInvalid),
			Message:       scanErr.Error(),
			ActionHint:    actionHintForCode(string(diagnostics.CodeConfigInvalid)),
		})
	}
	issues = append(issues, requestRewriteWarningIssues(cfg.RequestRewriteRulesSnapshot())...)
	path, existed := opencode.ResolveGlobalConfigPath()
	raw, loadErr := opencode.Load(path)
	fileSnapshotRaw := opencode.SnapshotFileConfig(path, existed, raw, loadErr, syncedProtocols())
	issues = append(issues, reconcileFileSnapshot(cfg, fileSnapshotRaw)...)
	runtimeSnapshotRaw := opencode.ReadRuntimeConfig(ctx, opencode.RuntimeReadOptions{RequestTimeout: 3 * time.Second, MaxRetries: 0})
	issues = append(issues, reconcileRuntimeSnapshot(cfg, fileSnapshotRaw, runtimeSnapshotRaw)...)
	issues = sortDoctorIssues(issues)
	report := DoctorReport{
		OK:                  len(issues) == 0,
		Issues:              issues,
		SyncProtocols:       syncedProtocols(),
		ConfigPath:          cfg.Path(),
		ProviderCount:       len(cfg.Providers),
		AliasCount:          len(cfg.Aliases),
		ProxyBindAddress:    proxyBindAddress(cfg),
		OpenCodeTargetPath:  path,
		OpenCodeTargetFound: existed,
		RuntimeBaseURL:      runtimeSnapshotRaw.BaseURL,
		RuntimeDirectory:    runtimeSnapshotRaw.Directory,
		FileSnapshot:        fileSnapshotView(fileSnapshotRaw, cfg),
		RuntimeSnapshot:     runtimeSnapshotView(runtimeSnapshotRaw),
		Summary:             summarizeReconciliation(cfg, fileSnapshotRaw, runtimeSnapshotRaw, issues),
	}
	if report.OK {
		return report, nil
	}
	return report, fmt.Errorf("%d config issue(s)", len(issues))
}

func (s *Service) PreviewOpenCodeSync(ctx context.Context, in SyncInput) (SyncPreview, error) {
	return s.previewOpenCodeSync(ctx, in, "")
}

func (s *Service) PreviewOpenCodeSyncDiff(ctx context.Context, in SyncInput) (SyncPreview, error) {
	return s.PreviewOpenCodeSync(ctx, in)
}

func (s *Service) PreviewOpenCodeSyncWithBaseURL(ctx context.Context, in SyncInput, publicBaseURL string) (SyncPreview, error) {
	return s.previewOpenCodeSync(ctx, in, publicBaseURL)
}

func (s *Service) previewOpenCodeSync(ctx context.Context, in SyncInput, publicBaseURL string) (SyncPreview, error) {
	prepared, err := s.prepareSync(ctx, in)
	if err != nil {
		return SyncPreview{}, err
	}
	if strings.TrimSpace(publicBaseURL) != "" && in.CopyOnly {
		prepared, err = s.prepareSyncWithBaseURL(ctx, in, publicBaseURL)
		if err != nil {
			return SyncPreview{}, err
		}
	}
	return SyncPreview{
		TargetPath:       prepared.targetPath,
		Protocols:        cloneSyncedProviders(prepared.protocols),
		SetModel:         in.SetModel,
		SetSmallModel:    in.SetSmallModel,
		Content:          prepared.content,
		WouldChange:      prepared.changed,
		RuntimeBaseURL:   prepared.runtimeBaseURL,
		RuntimeDirectory: prepared.runtimeDirectory,
		FileSnapshot:     prepared.fileSnapshot,
		RuntimeSnapshot:  prepared.runtimeSnapshot,
		DoctorIssues:     append([]DoctorIssue(nil), prepared.doctorIssues...),
		Summary:          prepared.summary,
		AliasPreviews:    cloneAliasSyncPreviews(prepared.aliasPreviews),
		OverallSummary:   cloneDiffSummaryView(prepared.overallSummary),
	}, nil
}

func (s *Service) ApplyOpenCodeSync(ctx context.Context, in SyncInput) (SyncResult, error) {
	prepared, err := s.prepareSync(ctx, in)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{
		TargetPath:       prepared.targetPath,
		Protocols:        cloneSyncedProviders(prepared.protocols),
		Changed:          prepared.changed,
		DryRun:           in.DryRun,
		SetModel:         in.SetModel,
		SetSmallModel:    in.SetSmallModel,
		Content:          prepared.content,
		RuntimeBaseURL:   prepared.runtimeBaseURL,
		RuntimeDirectory: prepared.runtimeDirectory,
		FileSnapshot:     prepared.fileSnapshot,
		RuntimeSnapshot:  prepared.runtimeSnapshot,
		DoctorIssues:     append([]DoctorIssue(nil), prepared.doctorIssues...),
		Summary:          prepared.summary,
		AliasPreviews:    cloneAliasSyncPreviews(prepared.aliasPreviews),
		OverallSummary:   cloneDiffSummaryView(prepared.overallSummary),
	}
	if !prepared.changed || in.DryRun {
		return result, nil
	}
	if err := opencode.Save(prepared.targetPath, prepared.raw); err != nil {
		return SyncResult{}, err
	}
	if cfg, cfgErr := s.loadConfig(); cfgErr == nil {
		result.FileSnapshot = fileSnapshotView(opencode.SnapshotFileConfig(prepared.targetPath, true, prepared.raw, nil, syncedProtocols()), cfg)
	}
	return result, nil
}

func (s *Service) StartProxy(ctx context.Context) error {
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return errs[0]
	}

	bindAddress := proxyBindAddress(cfg)

	s.mu.Lock()
	if s.proxyCancel != nil {
		ready := s.proxyReady
		done := s.proxyDone
		s.mu.Unlock()
		select {
		case <-ready:
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.proxyReadyErr
		case <-done:
			return s.WaitProxy(context.Background())
		case <-ctx.Done():
			_ = s.StopProxy(context.Background())
			return ctx.Err()
		}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan struct{})
	s.proxyCancel = cancel
	s.proxyDone = done
	s.proxyReady = ready
	s.proxyReadyErr = nil
	s.proxyErr = nil
	s.proxyStatus = ProxyStatusView{
		Running:     true,
		BindAddress: bindAddress,
		StartedAt:   formatTimestamp(time.Now()),
	}
	proxyServer := proxy.New(cfg, s.traces)
	s.proxyServer = proxyServer
	go s.runProxy(runCtx, done, ready, proxyServer, bindAddress)
	s.mu.Unlock()

	select {
	case <-ready:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.proxyReadyErr
	case <-done:
		return s.WaitProxy(context.Background())
	case <-ctx.Done():
		_ = s.StopProxy(context.Background())
		return ctx.Err()
	}
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

func (s *Service) GetProxySettings(ctx context.Context) (ProxySettingsView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ProxySettingsView{}, err
	}
	return proxySettingsView(cfg.Server), nil
}

func (s *Service) SaveProxySettings(ctx context.Context, in ProxySettingsInput) (ProxySettingsSaveResult, error) {
	if in.StreamPrecommitBufferMs < 0 {
		return ProxySettingsSaveResult{}, fmt.Errorf("server.stream_precommit_buffer_ms must be greater than or equal to 0")
	}
	if err := config.ValidateFailoverStatusCodes(in.FailoverStatusCodes); err != nil {
		return ProxySettingsSaveResult{}, err
	}
	normalizedRouting := routing.NormalizeConfig(routing.Config{Strategy: in.Routing.Strategy, Params: in.Routing.Params})
	if err := routing.ValidateConfig(normalizedRouting); err != nil {
		return ProxySettingsSaveResult{}, err
	}
	var out ProxySettingsSaveResult
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.Server.ConnectTimeoutMs = normalizePositiveInt(in.ConnectTimeoutMs, config.DefaultConnectTimeoutMs)
		cfg.Server.ResponseHeaderTimeoutMs = normalizePositiveInt(in.ResponseHeaderTimeoutMs, config.DefaultResponseHeaderTimeoutMs)
		cfg.Server.FirstByteTimeoutMs = normalizePositiveInt(in.FirstByteTimeoutMs, config.DefaultFirstByteTimeoutMs)
		cfg.Server.RequestReadTimeoutMs = normalizePositiveInt(in.RequestReadTimeoutMs, config.DefaultRequestReadTimeoutMs)
		cfg.Server.StreamIdleTimeoutMs = normalizePositiveInt(in.StreamIdleTimeoutMs, config.DefaultStreamIdleTimeoutMs)
		cfg.Server.StreamPrecommitBufferMs = in.StreamPrecommitBufferMs
		if in.ExcludeFirstTokenLatencyFromRate != nil {
			cfg.Server.ExcludeFirstTokenLatencyFromRate = *in.ExcludeFirstTokenLatencyFromRate
		}
		cfg.Server.FailoverStatusCodes = config.NormalizeFailoverStatusCodes(in.FailoverStatusCodes)
		cfg.Server.Routing = normalizedRouting
		warnings := []string{}
		status := s.currentProxyStatus(proxyBindAddress(cfg))
		if status.Running {
			warnings = append(warnings, "saved proxy timeout settings; restart proxy to apply changes")
		}
		out = ProxySettingsSaveResult{Settings: proxySettingsView(cfg.Server), Warnings: warnings}
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return out, err
}

func (s *Service) ListRequestTraces(ctx context.Context, limit int) ([]RequestTrace, error) {
	raw, err := s.traces.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RequestTrace, 0, len(raw))
	for _, trace := range raw {
		out = append(out, requestTraceView(trace))
	}
	return out, nil
}

func (s *Service) QueryRequestTraces(ctx context.Context, in RequestTraceListInput) (RequestTraceListResult, error) {
	startedFrom, err := parseOptionalTimestamp(in.StartedFrom)
	if err != nil {
		return RequestTraceListResult{}, fmt.Errorf("parse startedFrom: %w", err)
	}
	startedTo, err := parseOptionalTimestamp(in.StartedTo)
	if err != nil {
		return RequestTraceListResult{}, fmt.Errorf("parse startedTo: %w", err)
	}
	result, err := s.traces.Query(ctx, proxy.TraceQuery{
		Page:           in.Page,
		PageSize:       in.PageSize,
		Aliases:        in.Aliases,
		FailoverCounts: in.FailoverCounts,
		StatusCodes:    in.StatusCodes,
		StartedFrom:    startedFrom,
		StartedTo:      startedTo,
	})
	if err != nil {
		return RequestTraceListResult{}, err
	}
	return requestTraceListResultView(result), nil
}

func (s *Service) GetRequestTrace(ctx context.Context, id uint64) (RequestTrace, error) {
	trace, ok, err := s.traces.Get(ctx, id)
	if err != nil {
		return RequestTrace{}, err
	}
	if !ok {
		return RequestTrace{}, fmt.Errorf("request trace %d not found", id)
	}
	return requestTraceView(trace), nil
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
	var view DesktopPrefsView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.Desktop = config.Desktop{
			LaunchAtLogin:  in.LaunchAtLogin,
			AutoStartProxy: in.AutoStartProxy,
			MinimizeToTray: in.MinimizeToTray,
			Notifications:  in.Notifications,
			Theme:          normalizeThemePreference(in.Theme),
			Language:       normalizeLanguagePreference(in.Language),
		}
		view = desktopPrefsView(cfg.Desktop)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

type preparedSync struct {
	targetPath       string
	targetExisted    bool
	runtimeBaseURL   string
	runtimeDirectory string
	protocols        []SyncedProviderView
	aliasPreviews    []AliasSyncPreviewView
	overallSummary   *DiffSummaryView
	raw              opencode.Raw
	content          string
	changed          bool
	fileSnapshot     OpenCodeFileSnapshot
	runtimeSnapshot  OpenCodeRuntimeSnapshot
	doctorIssues     []DoctorIssue
	summary          OpenCodeReconciliationSummary
}

func (s *Service) prepareSync(ctx context.Context, in SyncInput) (preparedSync, error) {
	return s.prepareSyncWithBaseURL(ctx, in, "")
}

func (s *Service) prepareSyncWithBaseURL(ctx context.Context, in SyncInput, publicBaseURL string) (preparedSync, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return preparedSync{}, err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return preparedSync{}, errs[0]
	}
	targetPath := strings.TrimSpace(in.Target)
	runtimeBaseURL := strings.TrimSpace(in.RuntimeBaseURL)
	runtimeDirectory := strings.TrimSpace(in.RuntimeDirectory)
	_, targetExisted := opencode.ResolveGlobalConfigPath()
	if in.CopyOnly {
		targetExisted = false
		if targetPath == "" {
			targetPath = "opencode.jsonc"
		}
	} else if targetPath == "" {
		resolved, _ := opencode.ResolveGlobalConfigPath()
		targetPath = resolved
	}
	raw := opencode.Raw{}
	if !in.CopyOnly {
		loaded, err := opencode.Load(targetPath)
		if err != nil {
			return preparedSync{}, err
		}
		raw = loaded
	}
	protocolAliases := make(map[string][]string, len(syncedProtocols()))
	preparedProtocols := make([]SyncedProviderView, 0, len(syncedProtocols()))
	for _, protocol := range syncedProtocols() {
		aliasNames := cfg.AvailableAliasNamesForProtocol(protocol)
		sort.Strings(aliasNames)
		if !shouldSyncProtocol(raw, protocol, aliasNames) {
			continue
		}
		protocolAliases[protocol] = aliasNames
		preparedProtocols = append(preparedProtocols, SyncedProviderView{
			Key:        syncedProviderKey(protocol),
			Protocol:   protocol,
			AliasNames: append([]string(nil), aliasNames...),
		})
	}
	if err := validateSyncedModelSelection(in.SetModel, protocolAliases, "--set-model"); err != nil {
		return preparedSync{}, err
	}
	if err := validateSyncedModelSelection(in.SetSmallModel, protocolAliases, "--set-small-model"); err != nil {
		return preparedSync{}, err
	}
	aliasPreviews := make([]AliasSyncPreviewView, 0)
	overallSummary := &DiffSummaryView{}
	changed := false
	for _, prepared := range preparedProtocols {
		protocol := prepared.Protocol
		providerKey := prepared.Key
		baseURL := proxyBaseURLForProtocol(cfg, protocol)
		if strings.TrimSpace(publicBaseURL) != "" {
			baseURL = publicProxyBaseURLForProtocol(publicBaseURL, protocol)
		}
		aliasNames := protocolAliases[protocol]
		probeMap := make(map[string]opencode.ModelCapabilityProbe, len(aliasNames))
		for _, aliasName := range aliasNames {
			aliasCfg := cfg.FindAlias(aliasName)
			capability := opencode.ModelCapabilityProbe{ModelID: aliasName, Protocol: protocol, ProbeSource: opencode.ModelCapabilityProbeSourceFallback}
			if aliasCfg != nil {
				availableTargets := cfg.AvailableTargets(*aliasCfg)
				selectedTarget, targetProbe, ok := selectCapabilityProbeTarget(cfg, availableTargets)
				if ok {
					capability = opencode.ProbeModelCapability(ctx, targetProbe, selectedTarget.Model)
					capability.ModelID = selectedTarget.Model
				} else {
					capability.ProbeError = "no available provider target"
				}
			} else {
				capability.ProbeError = "alias not found"
			}
			probeMap[aliasName] = capability
			proposedCapability := capability
			proposedCapability.ModelID = aliasName
			proposedConfig := opencode.ModelConfigFromCapabilityProbe(proposedCapability)
			if pricing, ok := opencode.LookupModelPricing(aliasName); ok {
				proposedConfig["cost"] = map[string]any{
					"input":      pricing.InputPer1K,
					"output":     pricing.OutputPer1K,
					"cacheRead":  pricing.CacheReadPer1K,
					"cacheWrite": pricing.CacheWritePer1K,
				}
			}
			userModelConfig, _ := providerModelConfigForAlias(raw, providerKey, aliasName)
			probeErrors := map[string]string{}
			if strings.TrimSpace(capability.ProbeError) != "" {
				for _, path := range syncDiffFieldPathsForPreview() {
					probeErrors[path] = capability.ProbeError
				}
			}
			diff := opencode.ComputeSyncDiff(aliasName, protocol, userModelConfig, proposedConfig, probeErrors)
			aliasPreviews = append(aliasPreviews, aliasSyncPreviewView(providerKey, diff))
			overallSummary = addDiffSummaryView(overallSummary, diff.Summary)
		}
		if opencode.EnsureOcswitchProvider(protocol, raw, baseURL, cfg.Server.APIKey, aliasNames, probeMap) {
			changed = true
		}
	}
	if in.SetModel != "" && raw["model"] != in.SetModel {
		raw["model"] = in.SetModel
		changed = true
	}
	if in.SetSmallModel != "" && raw["small_model"] != in.SetSmallModel {
		raw["small_model"] = in.SetSmallModel
		changed = true
	}
	if len(aliasPreviews) == 0 {
		overallSummary = nil
	}
	fileSnapshotRaw := opencode.SnapshotFileConfig(targetPath, targetExisted, raw, nil, syncedProtocols())
	runtimeSnapshotRaw := opencode.ReadRuntimeConfig(ctx, opencode.RuntimeReadOptions{BaseURL: runtimeBaseURL, Directory: runtimeDirectory, RequestTimeout: 3 * time.Second, MaxRetries: 0})
	issues := reconcileFileSnapshot(cfg, fileSnapshotRaw)
	issues = append(issues, reconcileRuntimeSnapshot(cfg, fileSnapshotRaw, runtimeSnapshotRaw)...)
	issues = sortDoctorIssues(issues)
	content, err := marshalOpenCodeRaw(raw)
	if err != nil {
		return preparedSync{}, err
	}
	return preparedSync{
		targetPath:       targetPath,
		targetExisted:    targetExisted,
		runtimeBaseURL:   runtimeSnapshotRaw.BaseURL,
		runtimeDirectory: runtimeSnapshotRaw.Directory,
		protocols:        preparedProtocols,
		raw:              raw,
		content:          content,
		changed:          changed,
		fileSnapshot:     fileSnapshotView(fileSnapshotRaw, cfg),
		runtimeSnapshot:  runtimeSnapshotView(runtimeSnapshotRaw),
		doctorIssues:     issues,
		summary:          summarizeReconciliation(cfg, fileSnapshotRaw, runtimeSnapshotRaw, issues),
		aliasPreviews:    aliasPreviews,
		overallSummary:   overallSummary,
	}, nil
}

func (s *Service) loadConfig() (*config.Config, error) {
	return config.Load(s.configPath)
}

func (s *Service) reloadRunningProxyConfig(cfg *config.Config) error {
	s.mu.Lock()
	proxyServer := s.proxyServer
	running := s.proxyStatus.Running
	s.mu.Unlock()
	if !running || proxyServer == nil {
		return nil
	}
	return proxyServer.ReloadConfig(cfg)
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

func (s *Service) runProxy(runCtx context.Context, done chan struct{}, ready chan struct{}, proxyServer *proxy.Server, bindAddress string) {
	readyErr := make(chan error, 1)
	readyReported := make(chan struct{})
	go func() {
		boundErr := <-readyErr
		s.mu.Lock()
		if s.proxyReady == ready {
			s.proxyReadyErr = boundErr
		}
		close(ready)
		s.mu.Unlock()
		close(readyReported)
	}()
	err := proxyServer.ListenAndServeWithReady(runCtx, readyErr)
	<-readyReported
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxyDone == done {
		s.proxyCancel = nil
		s.proxyDone = nil
		s.proxyReady = nil
		s.proxyServer = nil
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

func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	return config.DefaultPath()
}

func providerView(provider config.Provider) ProviderView {
	apiKeys := provider.EffectiveAPIKeys()
	maskedKeys := make([]string, 0, len(apiKeys))
	for _, apiKey := range apiKeys {
		maskedKeys = append(maskedKeys, maskKey(apiKey))
	}
	return ProviderView{
		ID:               provider.ID,
		Name:             provider.Name,
		Protocol:         config.NormalizeProviderProtocol(provider.Protocol),
		BaseURL:          provider.BaseURL,
		BaseURLs:         append([]string(nil), provider.EffectiveBaseURLs()...),
		BaseURLStrategy:  config.NormalizeProviderBaseURLStrategy(provider.BaseURLStrategy),
		APIKeySet:        len(apiKeys) > 0,
		APIKeyMasked:     firstMaskedKey(maskedKeys),
		APIKeyCount:      len(apiKeys),
		APIKeysMasked:    maskedKeys,
		Headers:          cloneHeaders(provider.Headers),
		Models:           append([]string(nil), provider.Models...),
		ModelsSource:     provider.ModelsSource,
		Disabled:         provider.Disabled,
		AutoAliasEnabled: provider.EffectiveAutoAliasEnabled(),
	}
}

func firstMaskedKey(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func aliasView(cfg *config.Config, alias config.Alias) AliasView {
	targets := make([]AliasTargetView, 0, len(alias.Targets))
	for _, target := range alias.Targets {
		targets = append(targets, aliasTargetView(cfg, alias, target))
	}
	return AliasView{
		Alias:                alias.Alias,
		DisplayName:          alias.DisplayName,
		Protocol:             config.NormalizeAliasProtocol(alias.Protocol),
		Enabled:              alias.Enabled,
		TargetCount:          len(alias.Targets),
		AvailableTargetCount: len(cfg.AvailableTargets(alias)),
		Targets:              targets,
		AutoGenerated:        alias.AutoGenerated,
		Locked:               alias.Locked,
	}
}

func requestRewriteRuleViews(rules []config.RequestRewriteRule) []RequestRewriteRuleView {
	views := make([]RequestRewriteRuleView, 0, len(rules))
	for _, rule := range rules {
		views = append(views, requestRewriteRuleView(rule))
	}
	return views
}

func requestRewriteRuleView(rule config.RequestRewriteRule) RequestRewriteRuleView {
	return RequestRewriteRuleView{
		Name:      rule.Name,
		Alias:     rule.Alias,
		Providers: append([]string(nil), rule.Providers...),
		Enabled:   rule.Enabled,
		Override:  rule.Override,
		Ops:       cloneRequestRewriteOperations(rule.Ops),
		Legacy:    config.RequestRewriteRuleUsesLegacySyntax(rule),
		Warnings:  config.RequestRewriteRuleWarnings(rule),
	}
}

func doctorIssues(errs []error) []DoctorIssue {
	return structuralDoctorIssues(errs)
}

func requestRewriteWarningIssues(rules []config.RequestRewriteRule) []DoctorIssue {
	issues := []DoctorIssue{}
	for _, rule := range rules {
		for _, warning := range config.RequestRewriteRuleWarnings(rule) {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeRewriteRuleLegacy),
				Severity:       string(diagnostics.SeverityWarning),
				Reason:         string(diagnostics.ReasonLegacy),
				Message:        warning,
				Alias:          rule.Alias,
				ActionHint:     actionHintForCode(string(diagnostics.CodeRewriteRuleLegacy)),
				AllowedActions: []string{string(diagnostics.ActionMigrateRewriteRule), string(diagnostics.ActionKeep)},
				Params:         map[string]any{"ruleName": rule.Name, "alias": rule.Alias},
			})
		}
	}
	return issues
}

func desktopPrefsView(prefs config.Desktop) DesktopPrefsView {
	return DesktopPrefsView{
		LaunchAtLogin:  prefs.LaunchAtLogin,
		AutoStartProxy: prefs.AutoStartProxy,
		MinimizeToTray: prefs.MinimizeToTray,
		Notifications:  prefs.Notifications,
		Theme:          normalizeThemePreference(prefs.Theme),
		Language:       normalizeLanguagePreference(prefs.Language),
	}
}

func proxySettingsView(server config.Server) ProxySettingsView {
	return ProxySettingsView{
		ConnectTimeoutMs:                 normalizePositiveInt(server.ConnectTimeoutMs, config.DefaultConnectTimeoutMs),
		ResponseHeaderTimeoutMs:          normalizePositiveInt(server.ResponseHeaderTimeoutMs, config.DefaultResponseHeaderTimeoutMs),
		FirstByteTimeoutMs:               normalizePositiveInt(server.FirstByteTimeoutMs, config.DefaultFirstByteTimeoutMs),
		RequestReadTimeoutMs:             normalizePositiveInt(server.RequestReadTimeoutMs, config.DefaultRequestReadTimeoutMs),
		StreamIdleTimeoutMs:              normalizePositiveInt(server.StreamIdleTimeoutMs, config.DefaultStreamIdleTimeoutMs),
		StreamPrecommitBufferMs:          normalizeNonNegativeInt(server.StreamPrecommitBufferMs, config.DefaultStreamPrecommitBufferMs),
		ExcludeFirstTokenLatencyFromRate: server.ExcludeFirstTokenLatencyFromRate,
		FailoverStatusCodes:              config.NormalizeFailoverStatusCodes(server.FailoverStatusCodes),
		Routing:                          routingSettingsView(server.Routing),
	}
}

func routingInputJSON(value map[string]any) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func normalizePositiveInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalizeNonNegativeInt(value int, fallback int) int {
	if value < 0 {
		return fallback
	}
	return value
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
	case "system", "en-US", "zh-CN":
		return trimmed
	default:
		return "en-US"
	}
}

func validateSyncedModelSelection(value string, aliasesByProtocol map[string][]string, flagName string) error {
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
	choices := []string{}
	for _, aliases := range aliasesByProtocol {
		for _, name := range aliases {
			choices = append(choices, prefix+name)
			if name == alias {
				return nil
			}
		}
	}
	sort.Strings(choices)
	if len(choices) == 0 {
		return fmt.Errorf("%s requires at least one routable alias; run ocswitch alias list or doctor first", flagName)
	}
	return fmt.Errorf("%s %q is not a routable alias; available: %s", flagName, value, strings.Join(choices, ", "))
}

func syncedProtocols() []string {
	return []string{config.ProtocolOpenAIResponses, config.ProtocolAnthropicMessages, config.ProtocolOpenAICompatible}
}

func syncedProviderKey(protocol string) string {
	switch config.NormalizeProviderProtocol(protocol) {
	case config.ProtocolAnthropicMessages:
		return opencode.AnthropicProviderKey
	case config.ProtocolOpenAICompatible:
		return opencode.CompatProviderKey
	default:
		return opencode.ProviderKey
	}
}

func syncDiffFieldPathsForPreview() []string {
	return []string{
		"name",
		"limit.context",
		"limit.output",
		"cost.input",
		"cost.output",
		"cost.cacheRead",
		"cost.cacheWrite",
		"inputModalities",
		"outputModalities",
		"reasoning",
		"toolCall",
		"attachment",
		"temperature",
		"experimental",
		"variants",
		"status",
		"releaseDate",
	}
}

func selectCapabilityProbeTarget(cfg *config.Config, availableTargets []config.Target) (config.Target, opencode.ProviderModelProbeTarget, bool) {
	for _, target := range availableTargets {
		provider := cfg.FindProvider(target.Provider)
		if provider == nil {
			continue
		}
		return target, opencode.ProviderModelProbeTarget{
			ProviderID: provider.ID,
			Protocol:   provider.Protocol,
			BaseURLs:   provider.EffectiveBaseURLs(),
			APIKeys:    provider.EffectiveAPIKeys(),
			Headers:    cloneHeaders(provider.Headers),
		}, true
	}
	return config.Target{}, opencode.ProviderModelProbeTarget{}, false
}

func providerModelConfigForAlias(raw opencode.Raw, providerKey, aliasName string) (map[string]any, bool) {
	providerRaw, _ := raw["provider"].(map[string]any)
	if providerRaw == nil {
		return nil, false
	}
	providerEntry, _ := providerRaw[providerKey].(map[string]any)
	if providerEntry == nil {
		return nil, false
	}
	models, _ := providerEntry["models"].(map[string]any)
	if models == nil {
		return nil, false
	}
	model, _ := models[aliasName].(map[string]any)
	if model == nil {
		return nil, false
	}
	return cloneAnyMap(model), true
}

func aliasSyncPreviewView(providerKey string, diff opencode.AliasSyncDiff) AliasSyncPreviewView {
	entries := make([]SyncDiffEntryView, 0, len(diff.Entries))
	for _, entry := range diff.Entries {
		entries = append(entries, SyncDiffEntryView{
			Path:          entry.Path,
			UserValue:     cloneJSONValue(entry.UserValue),
			ProposedValue: cloneJSONValue(entry.ProposedValue),
			Status:        entry.Status,
			ConflictNote:  entry.ConflictNote,
			AutoDetected:  entry.AutoDetected,
		})
	}
	return AliasSyncPreviewView{
		AliasName:   diff.AliasName,
		Protocol:    diff.Protocol,
		ProviderKey: providerKey,
		Entries:     entries,
		Summary: DiffSummaryView{
			Total:     diff.Summary.Total,
			New:       diff.Summary.New,
			Changed:   diff.Summary.Changed,
			Unchanged: diff.Summary.Unchanged,
			Conflict:  diff.Summary.Conflict,
			Failed:    diff.Summary.Failed,
		},
	}
}

func addDiffSummaryView(base *DiffSummaryView, summary opencode.DiffSummary) *DiffSummaryView {
	if base == nil {
		base = &DiffSummaryView{}
	}
	base.Total += summary.Total
	base.New += summary.New
	base.Changed += summary.Changed
	base.Unchanged += summary.Unchanged
	base.Conflict += summary.Conflict
	base.Failed += summary.Failed
	return base
}

func cloneAliasSyncPreviews(in []AliasSyncPreviewView) []AliasSyncPreviewView {
	if len(in) == 0 {
		return nil
	}
	out := make([]AliasSyncPreviewView, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Entries = cloneSyncDiffEntryViews(in[i].Entries)
	}
	return out
}

func cloneSyncDiffEntryViews(in []SyncDiffEntryView) []SyncDiffEntryView {
	if len(in) == 0 {
		return nil
	}
	out := make([]SyncDiffEntryView, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].UserValue = cloneJSONValue(in[i].UserValue)
		out[i].ProposedValue = cloneJSONValue(in[i].ProposedValue)
	}
	return out
}

func cloneDiffSummaryView(in *DiffSummaryView) *DiffSummaryView {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func shouldSyncProtocol(raw opencode.Raw, protocol string, aliasNames []string) bool {
	if len(aliasNames) > 0 {
		return true
	}
	providerRaw, _ := raw["provider"].(map[string]any)
	if providerRaw == nil {
		return false
	}
	_, ok := providerRaw[syncedProviderKey(protocol)]
	return ok
}

func cloneSyncedProviders(in []SyncedProviderView) []SyncedProviderView {
	if len(in) == 0 {
		return nil
	}
	out := make([]SyncedProviderView, len(in))
	copy(out, in)
	for i := range out {
		out[i].AliasNames = append([]string(nil), out[i].AliasNames...)
	}
	return out
}

func marshalOpenCodeRaw(raw opencode.Raw) (string, error) {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal opencode config: %w", err)
	}
	return string(append(data, '\n')), nil
}

func proxyBindAddress(cfg *config.Config) string {
	return fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
}

func proxyBaseURLForProtocol(cfg *config.Config, protocol string) string {
	return fmt.Sprintf("http://%s:%d%s", cfg.Server.Host, cfg.Server.Port, config.ProtocolLocalBasePath(protocol))
}

func publicProxyBaseURLForProtocol(publicBaseURL string, protocol string) string {
	return strings.TrimRight(strings.TrimSpace(publicBaseURL), "/") + config.ProtocolLocalBasePath(protocol)
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

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneRequestRewriteOperations(in []config.RequestRewriteOperation) []config.RequestRewriteOperation {
	if len(in) == 0 {
		return nil
	}
	out := make([]config.RequestRewriteOperation, len(in))
	for i := range in {
		out[i] = config.RequestRewriteOperation{
			Op:       in[i].Op,
			Path:     in[i].Path,
			Value:    cloneJSONValue(in[i].Value),
			ValueSet: in[i].ValueSet,
		}
		if in[i].Index != nil {
			index := *in[i].Index
			out[i].Index = &index
		}
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for index := range typed {
			out[index] = cloneJSONValue(typed[index])
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
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

func reconcileFileSnapshot(cfg *config.Config, snapshot opencode.FileConfigSnapshot) []DoctorIssue {
	issues := []DoctorIssue{}
	if snapshot.ParseError != "" {
		issues = append(issues, DoctorIssue{
			SchemaVersion: diagnostics.SchemaVersion,
			Code:          string(diagnostics.CodeFileParseError),
			Severity:      string(diagnostics.SeverityError),
			Reason:        string(diagnostics.ReasonInvalid),
			Message:       snapshot.ParseError,
			Path:          snapshot.TargetPath,
			ActionHint:    actionHintForCode(string(diagnostics.CodeFileParseError)),
		})
		return issues
	}
	availableByProtocol := map[string][]string{}
	for _, protocol := range syncedProtocols() {
		available := cfg.AvailableAliasNamesForProtocol(protocol)
		sort.Strings(available)
		availableByProtocol[protocol] = available
	}
	for _, provider := range snapshot.SyncedProviders {
		wantAliases := availableByProtocol[provider.Protocol]
		wantBaseURL := proxyBaseURLForProtocol(cfg, provider.Protocol)
		if !provider.ContractConfigured && len(wantAliases) > 0 {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeOpenCodeContractMissing),
				Severity:       string(diagnostics.SeverityWarning),
				Reason:         string(diagnostics.ReasonMissing),
				Protocol:       provider.Protocol,
				ProviderKey:    provider.Key,
				Path:           snapshot.TargetPath,
				Message:        fmt.Sprintf("provider.%s missing from target file", provider.Key),
				Expected:       strings.Join(wantAliases, ", "),
				ActionHint:     actionHintForCode(string(diagnostics.CodeOpenCodeContractMissing)),
				AllowedActions: []string{string(diagnostics.ActionResyncOpenCode)},
			})
			continue
		}
		if len(provider.MissingFields) > 0 {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeOpenCodeContractInvalid),
				Severity:       string(diagnostics.SeverityError),
				Reason:         string(diagnostics.ReasonInvalid),
				Protocol:       provider.Protocol,
				ProviderKey:    provider.Key,
				Path:           snapshot.TargetPath,
				Message:        fmt.Sprintf("provider.%s contract incomplete in target file", provider.Key),
				Details:        append([]string(nil), provider.MissingFields...),
				ActionHint:     actionHintForCode(string(diagnostics.CodeOpenCodeContractInvalid)),
				AllowedActions: []string{string(diagnostics.ActionResyncOpenCode)},
			})
		}
		if provider.BaseURL != "" && provider.BaseURL != wantBaseURL {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeOpenCodeContractDrift),
				Severity:       string(diagnostics.SeverityError),
				Reason:         string(diagnostics.ReasonDrift),
				Protocol:       provider.Protocol,
				ProviderKey:    provider.Key,
				Path:           snapshot.TargetPath,
				Message:        fmt.Sprintf("provider.%s baseURL drift", provider.Key),
				Expected:       wantBaseURL,
				Actual:         provider.BaseURL,
				ActionHint:     actionHintForCode(string(diagnostics.CodeOpenCodeContractDrift)),
				AllowedActions: []string{string(diagnostics.ActionResyncOpenCode)},
			})
		}
		if !sameStringSet(provider.ModelAliases, wantAliases) {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeOpenCodeCatalogDrift),
				Severity:       string(diagnostics.SeverityWarning),
				Reason:         string(diagnostics.ReasonDrift),
				Protocol:       provider.Protocol,
				ProviderKey:    provider.Key,
				Path:           snapshot.TargetPath,
				Message:        fmt.Sprintf("provider.%s alias catalog drift", provider.Key),
				Expected:       strings.Join(wantAliases, ", "),
				Actual:         strings.Join(provider.ModelAliases, ", "),
				ActionHint:     actionHintForCode(string(diagnostics.CodeOpenCodeCatalogDrift)),
				AllowedActions: []string{string(diagnostics.ActionResyncOpenCode)},
			})
		}
	}
	issues = append(issues, validateDefaultModelSelections(snapshot.DefaultModel, snapshot.SmallModel, availableByProtocol, snapshot.TargetPath)...)
	return issues
}

func reconcileRuntimeSnapshot(cfg *config.Config, fileSnapshot opencode.FileConfigSnapshot, runtime opencode.RuntimeConfigSnapshot) []DoctorIssue {
	issues := []DoctorIssue{}
	if runtime.ErrorCode != "" {
		severity := string(diagnostics.SeverityWarning)
		reason := string(diagnostics.ReasonRuntimeUnavailable)
		if runtime.ErrorCode == "runtime_auth_failed" || runtime.ErrorCode == "runtime_bad_status" {
			severity = string(diagnostics.SeverityError)
		}
		if runtime.ErrorCode == "runtime_parse_error" {
			severity = string(diagnostics.SeverityError)
			reason = string(diagnostics.ReasonInvalid)
		}
		issues = append(issues, DoctorIssue{
			SchemaVersion:  diagnostics.SchemaVersion,
			Code:           runtime.ErrorCode,
			Severity:       severity,
			Reason:         reason,
			Message:        runtime.ErrorMessage,
			Directory:      runtime.Directory,
			Expected:       runtime.BaseURL,
			ActionHint:     actionHintForCode(runtime.ErrorCode),
			AllowedActions: []string{string(diagnostics.ActionRetryRuntime), string(diagnostics.ActionReloadRuntime)},
		})
		return issues
	}
	runtimeProviders := map[string]opencode.RuntimeProviderSnapshot{}
	for _, provider := range runtime.Providers {
		runtimeProviders[provider.ID] = provider
	}
	for _, fileProvider := range fileSnapshot.SyncedProviders {
		if !fileProvider.ContractConfigured {
			continue
		}
		runtimeProvider, ok := runtimeProviders[fileProvider.Key]
		if !ok {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeRuntimeProviderMissing),
				Severity:       string(diagnostics.SeverityWarning),
				Reason:         string(diagnostics.ReasonMissing),
				Protocol:       fileProvider.Protocol,
				ProviderKey:    fileProvider.Key,
				Directory:      runtime.Directory,
				Message:        fmt.Sprintf("runtime provider %s not exposed by OpenCode", fileProvider.Key),
				ActionHint:     actionHintForCode(string(diagnostics.CodeRuntimeProviderMissing)),
				AllowedActions: []string{string(diagnostics.ActionReloadRuntime), string(diagnostics.ActionRestartRuntime)},
			})
			continue
		}
		if fileProvider.NPM != "" && runtimeProvider.NPM != "" && fileProvider.NPM != runtimeProvider.NPM {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeRuntimeProviderProtocolMismatch),
				Severity:       string(diagnostics.SeverityError),
				Reason:         string(diagnostics.ReasonProtocolMismatch),
				Protocol:       fileProvider.Protocol,
				ProviderKey:    fileProvider.Key,
				Directory:      runtime.Directory,
				Message:        fmt.Sprintf("runtime provider %s npm drift", fileProvider.Key),
				Expected:       fileProvider.NPM,
				Actual:         runtimeProvider.NPM,
				ActionHint:     actionHintForCode(string(diagnostics.CodeRuntimeProviderProtocolMismatch)),
				AllowedActions: []string{string(diagnostics.ActionReloadRuntime), string(diagnostics.ActionRestartRuntime)},
			})
		}
		if !sameStringSet(fileProvider.ModelAliases, runtimeProvider.ModelIDs) {
			issues = append(issues, DoctorIssue{
				SchemaVersion:  diagnostics.SchemaVersion,
				Code:           string(diagnostics.CodeOpenCodeCatalogDrift),
				Severity:       string(diagnostics.SeverityWarning),
				Reason:         string(diagnostics.ReasonDrift),
				Protocol:       fileProvider.Protocol,
				ProviderKey:    fileProvider.Key,
				Directory:      runtime.Directory,
				Message:        fmt.Sprintf("runtime provider %s model catalog drift", fileProvider.Key),
				Expected:       strings.Join(fileProvider.ModelAliases, ", "),
				Actual:         strings.Join(runtimeProvider.ModelIDs, ", "),
				ActionHint:     "compare target file with runtime-loaded provider catalog",
				AllowedActions: []string{string(diagnostics.ActionResyncOpenCode), string(diagnostics.ActionReloadRuntime)},
			})
		}
	}
	availableByProtocol := map[string][]string{}
	for _, protocol := range syncedProtocols() {
		available := cfg.AvailableAliasNamesForProtocol(protocol)
		sort.Strings(available)
		availableByProtocol[protocol] = available
	}
	issues = append(issues, validateRuntimeDefaultModelSelections(runtime.DefaultModel, runtime.SmallModel, availableByProtocol, runtime.Directory)...)
	if fileSnapshot.DefaultModel != "" && runtime.DefaultModel != "" && fileSnapshot.DefaultModel != runtime.DefaultModel {
		issues = append(issues, DoctorIssue{
			SchemaVersion:  diagnostics.SchemaVersion,
			Code:           string(diagnostics.CodeOpenCodeCatalogDrift),
			Severity:       string(diagnostics.SeverityWarning),
			Reason:         string(diagnostics.ReasonDrift),
			Message:        "runtime default model differs from target file",
			Expected:       fileSnapshot.DefaultModel,
			Actual:         runtime.DefaultModel,
			Directory:      runtime.Directory,
			ActionHint:     "reload OpenCode runtime to pick up file changes",
			AllowedActions: []string{string(diagnostics.ActionReloadRuntime)},
		})
	}
	if fileSnapshot.SmallModel != "" && runtime.SmallModel != "" && fileSnapshot.SmallModel != runtime.SmallModel {
		issues = append(issues, DoctorIssue{
			SchemaVersion:  diagnostics.SchemaVersion,
			Code:           string(diagnostics.CodeOpenCodeCatalogDrift),
			Severity:       string(diagnostics.SeverityWarning),
			Reason:         string(diagnostics.ReasonDrift),
			Message:        "runtime small_model differs from target file",
			Expected:       fileSnapshot.SmallModel,
			Actual:         runtime.SmallModel,
			Directory:      runtime.Directory,
			ActionHint:     "reload OpenCode runtime to pick up file changes",
			AllowedActions: []string{string(diagnostics.ActionReloadRuntime)},
		})
	}
	return issues
}

func validateDefaultModelSelections(model string, smallModel string, aliasesByProtocol map[string][]string, path string) []DoctorIssue {
	issues := []DoctorIssue{}
	if issue := validateDefaultModelSelectionIssue(string(diagnostics.CodeOpenCodeDefaultUnroutable), model, aliasesByProtocol, path, "model"); issue != nil {
		issues = append(issues, *issue)
	}
	if issue := validateDefaultModelSelectionIssue(string(diagnostics.CodeOpenCodeSmallUnroutable), smallModel, aliasesByProtocol, path, "small_model"); issue != nil {
		issues = append(issues, *issue)
	}
	return issues
}

func validateRuntimeDefaultModelSelections(model string, smallModel string, aliasesByProtocol map[string][]string, directory string) []DoctorIssue {
	issues := []DoctorIssue{}
	if issue := validateDefaultModelSelectionIssue(string(diagnostics.CodeOpenCodeDefaultUnroutable), model, aliasesByProtocol, directory, "runtime model"); issue != nil {
		issue.Directory = directory
		issue.Path = ""
		issues = append(issues, *issue)
	}
	if issue := validateDefaultModelSelectionIssue(string(diagnostics.CodeOpenCodeSmallUnroutable), smallModel, aliasesByProtocol, directory, "runtime small_model"); issue != nil {
		issue.Directory = directory
		issue.Path = ""
		issues = append(issues, *issue)
	}
	return issues
}

func validateDefaultModelSelectionIssue(code string, value string, aliasesByProtocol map[string][]string, location string, fieldLabel string) *DoctorIssue {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	const prefix = "ocswitch/"
	if !strings.HasPrefix(value, prefix) {
		return &DoctorIssue{
			SchemaVersion:  diagnostics.SchemaVersion,
			Code:           code,
			Severity:       string(diagnostics.SeverityWarning),
			Reason:         string(diagnostics.ReasonMissing),
			Message:        fmt.Sprintf("%s %q does not point to ocswitch/<alias>", fieldLabel, value),
			Path:           location,
			ActionHint:     actionHintForCode(code),
			AllowedActions: []string{string(diagnostics.ActionSelectRoutableAlias), string(diagnostics.ActionClearExternalValue)},
		}
	}
	alias := strings.TrimPrefix(value, prefix)
	for _, aliases := range aliasesByProtocol {
		for _, candidate := range aliases {
			if candidate == alias {
				return nil
			}
		}
	}
	available := []string{}
	for _, aliases := range aliasesByProtocol {
		for _, aliasName := range aliases {
			available = append(available, prefix+aliasName)
		}
	}
	sort.Strings(available)
	return &DoctorIssue{
		SchemaVersion:  diagnostics.SchemaVersion,
		Code:           code,
		Severity:       string(diagnostics.SeverityWarning),
		Reason:         string(diagnostics.ReasonMissing),
		Alias:          alias,
		Message:        fmt.Sprintf("%s %q is not routable", fieldLabel, value),
		Path:           location,
		Expected:       strings.Join(available, ", "),
		ActionHint:     actionHintForCode(code),
		AllowedActions: []string{string(diagnostics.ActionSelectRoutableAlias), string(diagnostics.ActionClearExternalValue)},
	}
}

func sortDoctorIssues(in []DoctorIssue) []DoctorIssue {
	out := append([]DoctorIssue(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		left := doctorSeverityWeight(out[i].Severity)
		right := doctorSeverityWeight(out[j].Severity)
		if left != right {
			return left > right
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func doctorSeverityWeight(severity string) int {
	switch strings.TrimSpace(severity) {
	case "error":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func summarizeReconciliation(cfg *config.Config, fileSnapshot opencode.FileConfigSnapshot, runtime opencode.RuntimeConfigSnapshot, issues []DoctorIssue) OpenCodeReconciliationSummary {
	summary := OpenCodeReconciliationSummary{
		AvailableAliases:      sortedCopy(cfg.AvailableAliasNames()),
		RuntimeReachable:      runtime.Reachable,
		FileSnapshotAvailable: fileSnapshot.ParseError == "",
	}
	for _, issue := range issues {
		switch issue.Code {
		case string(diagnostics.CodeRuntimeProviderMissing):
			if issue.ProviderKey != "" {
				summary.MissingProviders = append(summary.MissingProviders, issue.ProviderKey)
			}
		case string(diagnostics.CodeOpenCodeDefaultUnroutable), string(diagnostics.CodeOpenCodeSmallUnroutable):
			summary.InvalidDefaultModels = append(summary.InvalidDefaultModels, issue.Message)
		case string(diagnostics.CodeOpenCodeCatalogDrift):
			summary.CatalogMismatches = append(summary.CatalogMismatches, issue.Message)
		}
	}
	fileProviders := map[string]bool{}
	for _, provider := range fileSnapshot.SyncedProviders {
		if provider.ContractConfigured {
			fileProviders[provider.Key] = true
		}
	}
	runtimeProviders := map[string]bool{}
	for _, provider := range runtime.Providers {
		runtimeProviders[provider.ID] = true
	}
	for key := range fileProviders {
		if !runtimeProviders[key] {
			summary.FileOnlyProviders = append(summary.FileOnlyProviders, key)
		}
	}
	for key := range runtimeProviders {
		if !fileProviders[key] && strings.HasPrefix(key, "ocswitch") {
			summary.RuntimeOnlyProviders = append(summary.RuntimeOnlyProviders, key)
		}
	}
	summary.MissingProviders = uniqueSorted(summary.MissingProviders)
	summary.InvalidDefaultModels = uniqueSorted(summary.InvalidDefaultModels)
	summary.CatalogMismatches = uniqueSorted(summary.CatalogMismatches)
	summary.FileOnlyProviders = uniqueSorted(summary.FileOnlyProviders)
	summary.RuntimeOnlyProviders = uniqueSorted(summary.RuntimeOnlyProviders)
	return summary
}

func fileSnapshotView(snapshot opencode.FileConfigSnapshot, cfg *config.Config) OpenCodeFileSnapshot {
	available := map[string]bool{}
	for _, alias := range cfg.AvailableAliasNames() {
		available[alias] = true
	}
	providers := make([]OpenCodeProviderSnapshot, 0, len(snapshot.SyncedProviders))
	for _, provider := range snapshot.SyncedProviders {
		providers = append(providers, OpenCodeProviderSnapshot{
			Key:                provider.Key,
			Name:               provider.Name,
			NPM:                provider.NPM,
			Protocol:           provider.Protocol,
			BaseURL:            provider.BaseURL,
			ModelAliases:       append([]string(nil), provider.ModelAliases...),
			MissingFields:      append([]string(nil), provider.MissingFields...),
			UnknownFieldKeys:   append([]string(nil), provider.UnknownFieldKeys...),
			RawJSONFragment:    provider.RawJSONFragment,
			ContractConfigured: provider.ContractConfigured,
		})
	}
	return OpenCodeFileSnapshot{
		TargetPath:           snapshot.TargetPath,
		Exists:               snapshot.Exists,
		Schema:               snapshot.Schema,
		DefaultModel:         snapshot.DefaultModel,
		SmallModel:           snapshot.SmallModel,
		ProviderKeys:         append([]string(nil), snapshot.ProviderKeys...),
		ExpectedProtocols:    append([]string(nil), snapshot.ExpectedProtocols...),
		SyncedProviders:      providers,
		UnknownTopLevelKeys:  append([]string(nil), snapshot.UnknownTopLevelKeys...),
		ParseError:           snapshot.ParseError,
		DefaultModelRoutable: defaultModelRoutable(snapshot.DefaultModel, available),
		SmallModelRoutable:   defaultModelRoutable(snapshot.SmallModel, available),
	}
}

func runtimeSnapshotView(snapshot opencode.RuntimeConfigSnapshot) OpenCodeRuntimeSnapshot {
	providers := make([]OpenCodeRuntimeProviderSnapshot, 0, len(snapshot.Providers))
	for _, provider := range snapshot.Providers {
		models := make([]OpenCodeRuntimeModelSnapshot, 0, len(provider.Models))
		for _, model := range provider.Models {
			models = append(models, OpenCodeRuntimeModelSnapshot{
				ID:               model.ID,
				Name:             model.Name,
				ProviderID:       model.ProviderID,
				ProviderNPM:      model.ProviderNPM,
				RawJSON:          model.RawJSON,
				ExtraFieldKeys:   append([]string(nil), model.ExtraFieldKeys...),
				OptionKeys:       append([]string(nil), model.OptionKeys...),
				Experimental:     model.Experimental,
				Reasoning:        model.Reasoning,
				ToolCall:         model.ToolCall,
				Temperature:      model.Temperature,
				Attachment:       model.Attachment,
				ContextLimit:     model.ContextLimit,
				OutputLimit:      model.OutputLimit,
				ReleaseDate:      model.ReleaseDate,
				Status:           model.Status,
				InputModalities:  append([]string(nil), model.InputModalities...),
				OutputModalities: append([]string(nil), model.OutputModalities...),
			})
		}
		providers = append(providers, OpenCodeRuntimeProviderSnapshot{
			ID:             provider.ID,
			Name:           provider.Name,
			API:            provider.API,
			NPM:            provider.NPM,
			Env:            append([]string(nil), provider.Env...),
			ModelIDs:       append([]string(nil), provider.ModelIDs...),
			Models:         models,
			ExtraFieldKeys: append([]string(nil), provider.ExtraFieldKeys...),
			RawJSON:        provider.RawJSON,
		})
	}
	defaultProviderModels := map[string]string{}
	for key, value := range snapshot.DefaultProviderModels {
		defaultProviderModels[key] = value
	}
	providerExtraFieldMap := map[string][]string{}
	for key, values := range snapshot.ProviderExtraFieldMap {
		providerExtraFieldMap[key] = append([]string(nil), values...)
	}
	return OpenCodeRuntimeSnapshot{
		BaseURL:               snapshot.BaseURL,
		Directory:             snapshot.Directory,
		Reachable:             snapshot.Reachable,
		ConfigLoaded:          snapshot.ConfigLoaded,
		ProvidersLoaded:       snapshot.ProvidersLoaded,
		DefaultModel:          snapshot.DefaultModel,
		SmallModel:            snapshot.SmallModel,
		ProviderKeys:          append([]string(nil), snapshot.ProviderKeys...),
		DefaultProviderModels: defaultProviderModels,
		Providers:             providers,
		ErrorCode:             snapshot.ErrorCode,
		ErrorMessage:          snapshot.ErrorMessage,
		HTTPStatus:            snapshot.HTTPStatus,
		RawConfigJSON:         snapshot.RawConfigJSON,
		RawProvidersJSON:      snapshot.RawProvidersJSON,
		ConfigExtraFieldKeys:  append([]string(nil), snapshot.ConfigExtraFieldKeys...),
		ProviderExtraFieldMap: providerExtraFieldMap,
	}
}

func sameStringSet(left []string, right []string) bool {
	return strings.Join(uniqueSorted(left), "\x00") == strings.Join(uniqueSorted(right), "\x00")
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func defaultModelRoutable(value string, available map[string]bool) bool {
	if value == "" || !strings.HasPrefix(value, "ocswitch/") {
		return false
	}
	return available[strings.TrimPrefix(value, "ocswitch/")]
}
