package desktop

import (
	"context"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

// Bindings is the thin desktop-callable facade shared by the fallback HTTP shell
// and the Wails bridge.
type Bindings struct {
	service *app.Service
}

func NewBindings(service *app.Service) *Bindings {
	return &Bindings{service: service}
}

func (b *Bindings) GetOverview(ctx context.Context) (app.Overview, error) {
	return b.service.GetOverview(ctx)
}

func (b *Bindings) ExportConfig(ctx context.Context) (app.ConfigExportView, error) {
	return b.service.ExportConfig(ctx)
}

func (b *Bindings) ImportConfig(ctx context.Context, in app.ConfigImportInput) (app.ConfigImportResult, error) {
	return b.service.ImportConfig(ctx, in)
}

func (b *Bindings) ListProviders(ctx context.Context) ([]app.ProviderView, error) {
	return b.service.ListProviders(ctx)
}

func (b *Bindings) ListAliases(ctx context.Context) ([]app.AliasView, error) {
	return b.service.ListAliases(ctx)
}

func (b *Bindings) UpsertProvider(ctx context.Context, in app.ProviderUpsertInput) (app.ProviderSaveResult, error) {
	return b.service.UpsertProvider(ctx, in)
}

func (b *Bindings) RefreshProviderModels(ctx context.Context, in app.ProviderRefreshModelsInput) (app.ProviderSaveResult, error) {
	return b.service.RefreshProviderModels(ctx, in)
}

func (b *Bindings) PingProviderBaseURL(ctx context.Context, in app.ProviderPingInput) (app.ProviderPingResult, error) {
	return b.service.PingProviderBaseURL(ctx, in)
}

func (b *Bindings) SetProviderDisabled(ctx context.Context, in app.ProviderStateInput) (app.ProviderView, error) {
	return b.service.SetProviderDisabled(ctx, in)
}

func (b *Bindings) RemoveProvider(ctx context.Context, id string) error {
	return b.service.RemoveProvider(ctx, id)
}

func (b *Bindings) ImportProviders(ctx context.Context, in app.ProviderImportInput) (app.ProviderImportResult, error) {
	return b.service.ImportProviders(ctx, in)
}

func (b *Bindings) UpsertAlias(ctx context.Context, in app.AliasUpsertInput) (app.AliasView, error) {
	return b.service.UpsertAlias(ctx, in)
}

func (b *Bindings) RemoveAlias(ctx context.Context, name string) error {
	return b.service.RemoveAlias(ctx, name)
}

func (b *Bindings) BindAliasTarget(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return b.service.BindAliasTarget(ctx, in)
}

func (b *Bindings) SetAliasTargetDisabled(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return b.service.SetAliasTargetDisabled(ctx, in)
}

func (b *Bindings) UnbindAliasTarget(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return b.service.UnbindAliasTarget(ctx, in)
}

func (b *Bindings) ReorderAliasTargets(ctx context.Context, in app.AliasTargetReorderInput) (app.AliasView, error) {
	return b.service.ReorderAliasTargets(ctx, in)
}

func (b *Bindings) RunDoctor(ctx context.Context) (app.DoctorReport, error) {
	return b.service.RunDoctor(ctx)
}

func (b *Bindings) SyncOpenCode(ctx context.Context, in app.SyncInput) (app.SyncResult, error) {
	return b.service.ApplyOpenCodeSync(ctx, in)
}

func (b *Bindings) PreviewOpenCodeSync(ctx context.Context, in app.SyncInput) (app.SyncPreview, error) {
	return b.service.PreviewOpenCodeSync(ctx, in)
}

func (b *Bindings) GetProxyStatus(ctx context.Context) (app.ProxyStatusView, error) {
	return b.service.GetProxyStatus(ctx)
}

func (b *Bindings) ListRequestTraces(ctx context.Context, limit int) ([]app.RequestTrace, error) {
	return b.service.ListRequestTraces(ctx, limit)
}

func (b *Bindings) QueryRequestTraces(ctx context.Context, in app.RequestTraceListInput) (app.RequestTraceListResult, error) {
	return b.service.QueryRequestTraces(ctx, in)
}

func (b *Bindings) GetProxySettings(ctx context.Context) (app.ProxySettingsView, error) {
	return b.service.GetProxySettings(ctx)
}

func (b *Bindings) SaveProxySettings(ctx context.Context, in app.ProxySettingsInput) (app.ProxySettingsSaveResult, error) {
	return b.service.SaveProxySettings(ctx, in)
}

func (b *Bindings) StartProxy(ctx context.Context) (app.ProxyStatusView, error) {
	if err := b.service.StartProxy(ctx); err != nil {
		return app.ProxyStatusView{}, err
	}
	return b.service.GetProxyStatus(ctx)
}

func (b *Bindings) StopProxy(ctx context.Context) (app.ProxyStatusView, error) {
	if err := b.service.StopProxy(ctx); err != nil {
		return app.ProxyStatusView{}, err
	}
	return b.service.GetProxyStatus(ctx)
}

func (b *Bindings) Overview() (app.Overview, error) {
	return b.GetOverview(context.Background())
}

func (b *Bindings) ExportConfigNow() (app.ConfigExportView, error) {
	return b.ExportConfig(context.Background())
}

func (b *Bindings) ImportConfigNow(in app.ConfigImportInput) (app.ConfigImportResult, error) {
	return b.ImportConfig(context.Background(), in)
}

func (b *Bindings) Providers() ([]app.ProviderView, error) {
	return b.ListProviders(context.Background())
}

func (b *Bindings) Aliases() ([]app.AliasView, error) {
	return b.ListAliases(context.Background())
}

func (b *Bindings) SaveProvider(in app.ProviderUpsertInput) (app.ProviderSaveResult, error) {
	return b.UpsertProvider(context.Background(), in)
}

func (b *Bindings) RefreshProviderModelsNow(in app.ProviderRefreshModelsInput) (app.ProviderSaveResult, error) {
	return b.RefreshProviderModels(context.Background(), in)
}

func (b *Bindings) PingProviderBaseURLNow(in app.ProviderPingInput) (app.ProviderPingResult, error) {
	return b.PingProviderBaseURL(context.Background(), in)
}

func (b *Bindings) SetProviderState(in app.ProviderStateInput) (app.ProviderView, error) {
	return b.SetProviderDisabled(context.Background(), in)
}

func (b *Bindings) DeleteProvider(id string) error {
	return b.RemoveProvider(context.Background(), id)
}

func (b *Bindings) ImportProviderSet(in app.ProviderImportInput) (app.ProviderImportResult, error) {
	return b.ImportProviders(context.Background(), in)
}

func (b *Bindings) SaveAlias(in app.AliasUpsertInput) (app.AliasView, error) {
	return b.UpsertAlias(context.Background(), in)
}

func (b *Bindings) DeleteAlias(name string) error {
	return b.RemoveAlias(context.Background(), name)
}

func (b *Bindings) BindTarget(in app.AliasTargetInput) (app.AliasView, error) {
	return b.BindAliasTarget(context.Background(), in)
}

func (b *Bindings) SetTargetState(in app.AliasTargetInput) (app.AliasView, error) {
	return b.SetAliasTargetDisabled(context.Background(), in)
}

func (b *Bindings) UnbindTarget(in app.AliasTargetInput) (app.AliasView, error) {
	return b.UnbindAliasTarget(context.Background(), in)
}

func (b *Bindings) ReorderTargets(in app.AliasTargetReorderInput) (app.AliasView, error) {
	return b.ReorderAliasTargets(context.Background(), in)
}

func (b *Bindings) Doctor() (app.DoctorReport, error) {
	return b.RunDoctor(context.Background())
}

func (b *Bindings) ProxyStatus() (app.ProxyStatusView, error) {
	return b.GetProxyStatus(context.Background())
}

func (b *Bindings) RequestTraces(limit int) ([]app.RequestTrace, error) {
	return b.ListRequestTraces(context.Background(), limit)
}

func (b *Bindings) TraceList(in app.RequestTraceListInput) (app.RequestTraceListResult, error) {
	return b.QueryRequestTraces(context.Background(), in)
}

func (b *Bindings) ProxySettings() (app.ProxySettingsView, error) {
	return b.GetProxySettings(context.Background())
}

func (b *Bindings) SaveProxyConfig(in app.ProxySettingsInput) (app.ProxySettingsSaveResult, error) {
	return b.SaveProxySettings(context.Background(), in)
}

func (b *Bindings) StartProxyNow() (app.ProxyStatusView, error) {
	return b.StartProxy(context.Background())
}

func (b *Bindings) StopProxyNow() (app.ProxyStatusView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.StopProxy(ctx)
}

func (b *Bindings) DesktopPrefs() (app.DesktopPrefsView, error) {
	return b.GetDesktopPrefs(context.Background())
}

func (b *Bindings) SavePrefs(in app.DesktopPrefsInput) (app.DesktopPrefsView, error) {
	return b.SaveDesktopPrefs(context.Background(), in)
}

func (b *Bindings) PreviewSync(in app.SyncInput) (app.SyncPreview, error) {
	return b.PreviewOpenCodeSync(context.Background(), in)
}

func (b *Bindings) ApplySync(in app.SyncInput) (app.SyncResult, error) {
	return b.SyncOpenCode(context.Background(), in)
}

func (b *Bindings) GetDesktopPrefs(ctx context.Context) (app.DesktopPrefsView, error) {
	return b.service.GetDesktopPrefs(ctx)
}

func (b *Bindings) SaveDesktopPrefs(ctx context.Context, in app.DesktopPrefsInput) (app.DesktopPrefsView, error) {
	return b.service.SaveDesktopPrefs(ctx, in)
}
