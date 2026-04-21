package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

type trayAdapter interface {
	Attach(context.Context)
	Detach()
	Sync(context.Context, app.DesktopPrefsView)
	RefreshProxyStatus(context.Context)
	BeforeClose(context.Context) (bool, error)
}

type notifierAdapter interface {
	Attach(context.Context)
	Detach()
	Sync(context.Context, app.DesktopPrefsView)
	Send(context.Context, string, string) error
}

type autoStartAdapter interface {
	Attach(context.Context)
	Detach()
	Sync(context.Context, app.DesktopPrefsView) error
}

// App is the desktop-shell composition root. Native shell integrations are kept
// out of internal/app so CLI and GUI can share the same workflows.
type App struct {
	service  *app.Service
	bindings *Bindings
	tray     trayAdapter
	notify   notifierAdapter
	auto     autoStartAdapter

	ctx     context.Context
	version string
}

func New(configPath string) *App {
	svc := app.NewService(configPath)
	instance := &App{service: svc}
	instance.bindings = NewBindings(svc)
	instance.tray = NewTray(svc)
	instance.notify = NewNotifier(svc)
	instance.auto = NewAutoStart(svc)
	return instance
}

func (a *App) SetVersion(version string) {
	a.version = version
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.tray.Attach(ctx)
	a.notify.Attach(ctx)
	a.auto.Attach(ctx)
	_ = a.SyncDesktopPreferences(ctx)
	if prefs, err := a.bindings.GetDesktopPrefs(ctx); err == nil && prefs.AutoStartProxy {
		_, _ = a.bindings.StartProxy(ctx)
		a.watchAutoStartProxyFailure()
	}
	a.tray.RefreshProxyStatus(ctx)
}

func (a *App) watchAutoStartProxyFailure() {
	go func() {
		waitCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		err := a.service.WaitProxy(waitCtx)
		if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return
		}
		callCtx := a.callContext()
		a.tray.RefreshProxyStatus(callCtx)
		_ = a.notify.Send(callCtx, "Proxy failed to start", err.Error())
	}()
}

func (a *App) BeforeClose(ctx context.Context) bool {
	a.ctx = ctx
	prevent, _ := a.tray.BeforeClose(ctx)
	return prevent
}

func (a *App) Shutdown(ctx context.Context) {
	a.ctx = ctx
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.service.StopProxy(shutdownCtx)
	a.tray.Detach()
	a.notify.Detach()
	a.auto.Detach()
}

func (a *App) SyncDesktopPreferences(ctx context.Context) error {
	prefs, err := a.bindings.GetDesktopPrefs(ctx)
	if err != nil {
		return err
	}
	autoErr := a.auto.Sync(ctx, prefs)
	a.tray.Sync(ctx, prefs)
	a.notify.Sync(ctx, prefs)
	return autoErr
}

func (a *App) SaveDesktopPrefs(ctx context.Context, in app.DesktopPrefsInput) (app.DesktopPrefsSaveResult, error) {
	previous, _ := a.bindings.GetDesktopPrefs(ctx)
	prefs, err := a.bindings.SaveDesktopPrefs(ctx, in)
	if err != nil {
		return app.DesktopPrefsSaveResult{}, err
	}
	warnings := []string{}
	if err := a.auto.Sync(ctx, prefs); err != nil {
		warnings = append(warnings, fmt.Sprintf("saved preferences but could not update launch-at-login integration: %v", err))
	}
	a.tray.Sync(ctx, prefs)
	a.notify.Sync(ctx, prefs)
	if prefs.Notifications && !previous.Notifications {
		_ = a.notify.Send(ctx, "Notifications enabled", "ocswitch desktop will now show native alerts.")
	}
	return app.DesktopPrefsSaveResult{Prefs: prefs, Warnings: warnings}, nil
}

func (a *App) SavePrefs(in app.DesktopPrefsInput) (app.DesktopPrefsSaveResult, error) {
	return a.SaveDesktopPrefs(a.callContext(), in)
}

func (a *App) Meta() map[string]string {
	return map[string]string{
		"version": a.version,
		"shell":   a.shellName(),
	}
}

func (a *App) OpenExternalURL(rawURL string) error {
	url := strings.TrimSpace(rawURL)
	if url == "" {
		return nil
	}
	return openExternalURL(a.callContext(), url)
}

func (a *App) Overview() (app.Overview, error) {
	return a.bindings.GetOverview(a.callContext())
}

func (a *App) ExportConfig() (app.ConfigExportView, error) {
	return a.bindings.ExportConfig(a.callContext())
}

func (a *App) ImportConfig(in app.ConfigImportInput) (app.ConfigImportResult, error) {
	result, err := a.bindings.ImportConfig(a.callContext(), in)
	if err != nil {
		return app.ConfigImportResult{}, err
	}
	if syncErr := a.SyncDesktopPreferences(a.callContext()); syncErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("imported config but could not sync desktop integrations: %v", syncErr))
	}
	a.tray.RefreshProxyStatus(a.callContext())
	return result, nil
}

func (a *App) Providers() ([]app.ProviderView, error) {
	return a.bindings.ListProviders(a.callContext())
}

func (a *App) Aliases() ([]app.AliasView, error) {
	return a.bindings.ListAliases(a.callContext())
}

func (a *App) SaveProvider(in app.ProviderUpsertInput) (app.ProviderSaveResult, error) {
	return a.bindings.UpsertProvider(a.callContext(), in)
}

func (a *App) RefreshProviderModels(in app.ProviderRefreshModelsInput) (app.ProviderSaveResult, error) {
	return a.bindings.RefreshProviderModels(a.callContext(), in)
}

func (a *App) SetProviderState(in app.ProviderStateInput) (app.ProviderView, error) {
	return a.bindings.SetProviderDisabled(a.callContext(), in)
}

func (a *App) DeleteProvider(id string) error {
	return a.bindings.RemoveProvider(a.callContext(), id)
}

func (a *App) ImportProviders(in app.ProviderImportInput) (app.ProviderImportResult, error) {
	return a.bindings.ImportProviders(a.callContext(), in)
}

func (a *App) SaveAlias(in app.AliasUpsertInput) (app.AliasView, error) {
	return a.bindings.UpsertAlias(a.callContext(), in)
}

func (a *App) DeleteAlias(name string) error {
	return a.bindings.RemoveAlias(a.callContext(), name)
}

func (a *App) BindTarget(in app.AliasTargetInput) (app.AliasView, error) {
	return a.bindings.BindAliasTarget(a.callContext(), in)
}

func (a *App) SetTargetState(in app.AliasTargetInput) (app.AliasView, error) {
	return a.bindings.SetAliasTargetDisabled(a.callContext(), in)
}

func (a *App) UnbindTarget(in app.AliasTargetInput) (app.AliasView, error) {
	return a.bindings.UnbindAliasTarget(a.callContext(), in)
}

func (a *App) DoctorRun() (app.DoctorRunResult, error) {
	report, err := a.bindings.RunDoctor(a.callContext())
	return app.DoctorRunResult{Report: report, Error: errorString(err)}, nil
}

func (a *App) ProxyStatus() (app.ProxyStatusView, error) {
	return a.bindings.GetProxyStatus(a.callContext())
}

func (a *App) RequestTraces(limit int) ([]app.RequestTrace, error) {
	return a.bindings.ListRequestTraces(a.callContext(), limit)
}

func (a *App) TraceList(in app.RequestTraceListInput) (app.RequestTraceListResult, error) {
	return a.bindings.QueryRequestTraces(a.callContext(), in)
}

func (a *App) ProxySettings() (app.ProxySettingsView, error) {
	return a.bindings.GetProxySettings(a.callContext())
}

func (a *App) SaveProxySettings(in app.ProxySettingsInput) (app.ProxySettingsSaveResult, error) {
	return a.bindings.SaveProxySettings(a.callContext(), in)
}

func (a *App) StartProxy() (app.ProxyStatusView, error) {
	status, err := a.bindings.StartProxy(a.callContext())
	if err != nil {
		return app.ProxyStatusView{}, err
	}
	a.tray.RefreshProxyStatus(a.callContext())
	_ = a.notify.Send(a.callContext(), "Proxy started", status.BindAddress)
	return status, nil
}

func (a *App) StopProxy() (app.ProxyStatusView, error) {
	ctx, cancel := context.WithTimeout(a.callContext(), 5*time.Second)
	defer cancel()
	status, err := a.bindings.StopProxy(ctx)
	if err != nil {
		return app.ProxyStatusView{}, err
	}
	a.tray.RefreshProxyStatus(a.callContext())
	_ = a.notify.Send(a.callContext(), "Proxy stopped", status.BindAddress)
	return status, nil
}

func (a *App) DesktopPrefs() (app.DesktopPrefsView, error) {
	return a.bindings.GetDesktopPrefs(a.callContext())
}

func (a *App) PreviewSync(in app.SyncInput) (app.SyncPreview, error) {
	return a.bindings.PreviewOpenCodeSync(a.callContext(), in)
}

func (a *App) ApplySync(in app.SyncInput) (app.SyncResult, error) {
	result, err := a.bindings.SyncOpenCode(a.callContext(), in)
	if err != nil {
		return app.SyncResult{}, err
	}
	if result.Changed && !result.DryRun {
		_ = a.notify.Send(a.callContext(), "OpenCode sync applied", result.TargetPath)
	}
	return result, nil
}

func (a *App) callContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) shellName() string {
	if a.ctx != nil {
		return "wails"
	}
	return "browser"
}

func (a *App) Service() *app.Service {
	return a.service
}

func (a *App) Bindings() *Bindings {
	return a.bindings
}
