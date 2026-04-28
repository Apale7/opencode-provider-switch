package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/webadmin"
)

type RunOptions struct {
	ConfigPath   string
	Version      string
	Host         string
	Port         int
	ShutdownWait time.Duration
	Logger       *log.Logger
}

func Run(opts RunOptions) error {
	if opts.ShutdownWait <= 0 {
		opts.ShutdownWait = 5 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "[ocswitch-server] ", log.LstdFlags|log.Lmicroseconds)
	}

	cfg, generated, err := ensureAdminConfig(opts)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", cfg.Admin.Host, cfg.Admin.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen server admin: %w", err)
	}
	defer listener.Close()

	baseURL := "http://" + listener.Addr().String()
	svc := app.NewService(cfg.Path())
	handler, err := webadmin.NewHandler(webadmin.Options{
		Version:       opts.Version,
		Shell:         "server",
		BaseURL:       baseURL,
		Service:       appService{service: svc, publicBaseURL: cfg.Admin.PublicBaseURL},
		Auth:          adminAuth(cfg.Admin.APIKey),
		SecureHeaders: true,
		ServerMode:    true,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	logger.Printf("admin web listening: %s", baseURL)
	logger.Printf("config path: %s", cfg.Path())
	if !isLoopbackHost(cfg.Admin.Host) {
		logger.Printf("warning: admin web is listening on non-loopback host %q; use firewall or HTTPS reverse proxy", cfg.Admin.Host)
	}
	if generated {
		logger.Printf("admin API key generated and saved in config admin.api_key")
		logger.Printf("Authorization: Bearer %s", cfg.Admin.APIKey)
	} else {
		logger.Printf("admin API key loaded from config admin.api_key")
	}
	if err := svc.StartProxy(context.Background()); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}
	proxyStatus, _ := svc.GetProxyStatus(context.Background())
	logger.Printf("proxy listening: %s", proxyStatus.BindAddress)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownWait)
		defer cancel()
		_ = svc.StopProxy(shutdownCtx)
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func ensureAdminConfig(opts RunOptions) (*config.Config, bool, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(opts.Host) != "" {
		cfg.Admin.Host = strings.TrimSpace(opts.Host)
	}
	if opts.Port != 0 {
		cfg.Admin.Port = opts.Port
	}
	if strings.TrimSpace(cfg.Admin.Host) == "" {
		cfg.Admin.Host = "127.0.0.1"
	}
	if cfg.Admin.Port == 0 {
		cfg.Admin.Port = 9983
	}
	generated := false
	if strings.TrimSpace(cfg.Admin.APIKey) == "" {
		key, err := generateAdminAPIKey()
		if err != nil {
			return nil, false, err
		}
		cfg.Admin.APIKey = key
		generated = true
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, false, errs[0]
	}
	if generated || strings.TrimSpace(opts.Host) != "" || opts.Port != 0 {
		if err := cfg.Save(); err != nil {
			return nil, false, err
		}
	}
	return cfg, generated, nil
}

func generateAdminAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate admin api key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func adminAuth(expected string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			token = r.Header.Get("X-Api-Key")
		}
		if constantTimeEqual(token, expected) {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="ocswitch-admin"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
		return false
	}
}

func bearerToken(header string) string {
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func constantTimeEqual(got string, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	gotBytes := []byte(got)
	expectedBytes := []byte(expected)
	if len(gotBytes) != len(expectedBytes) {
		return false
	}
	return subtle.ConstantTimeCompare(gotBytes, expectedBytes) == 1
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type appService struct {
	service       *app.Service
	publicBaseURL string
}

func (s appService) GetOverview(ctx context.Context) (app.Overview, error) {
	return s.service.GetOverview(ctx)
}

func (s appService) ExportConfig(ctx context.Context) (app.ConfigExportView, error) {
	return s.service.ExportConfig(ctx)
}

func (s appService) ImportConfig(ctx context.Context, in app.ConfigImportInput) (app.ConfigImportResult, error) {
	return s.service.ImportConfig(ctx, in)
}

func (s appService) ListProviders(ctx context.Context) ([]app.ProviderView, error) {
	return s.service.ListProviders(ctx)
}

func (s appService) UpsertProvider(ctx context.Context, in app.ProviderUpsertInput) (app.ProviderSaveResult, error) {
	return s.service.UpsertProvider(ctx, in)
}

func (s appService) ImportProviders(ctx context.Context, in app.ProviderImportInput) (app.ProviderImportResult, error) {
	return s.service.ImportProviders(ctx, in)
}

func (s appService) RefreshProviderModels(ctx context.Context, in app.ProviderRefreshModelsInput) (app.ProviderSaveResult, error) {
	return s.service.RefreshProviderModels(ctx, in)
}

func (s appService) PingProviderBaseURL(ctx context.Context, in app.ProviderPingInput) (app.ProviderPingResult, error) {
	return s.service.PingProviderBaseURL(ctx, in)
}

func (s appService) SetProviderDisabled(ctx context.Context, in app.ProviderStateInput) (app.ProviderView, error) {
	return s.service.SetProviderDisabled(ctx, in)
}

func (s appService) RemoveProvider(ctx context.Context, id string) error {
	return s.service.RemoveProvider(ctx, id)
}

func (s appService) ListAliases(ctx context.Context) ([]app.AliasView, error) {
	return s.service.ListAliases(ctx)
}

func (s appService) UpsertAlias(ctx context.Context, in app.AliasUpsertInput) (app.AliasView, error) {
	return s.service.UpsertAlias(ctx, in)
}

func (s appService) RemoveAlias(ctx context.Context, name string) error {
	return s.service.RemoveAlias(ctx, name)
}

func (s appService) BindAliasTarget(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return s.service.BindAliasTarget(ctx, in)
}

func (s appService) SetAliasTargetDisabled(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return s.service.SetAliasTargetDisabled(ctx, in)
}

func (s appService) UnbindAliasTarget(ctx context.Context, in app.AliasTargetInput) (app.AliasView, error) {
	return s.service.UnbindAliasTarget(ctx, in)
}

func (s appService) ReorderAliasTargets(ctx context.Context, in app.AliasTargetReorderInput) (app.AliasView, error) {
	return s.service.ReorderAliasTargets(ctx, in)
}

func (s appService) GetDesktopPrefs(ctx context.Context) (app.DesktopPrefsView, error) {
	return s.service.GetDesktopPrefs(ctx)
}

func (s appService) SaveDesktopPrefs(ctx context.Context, in app.DesktopPrefsInput) (app.DesktopPrefsView, error) {
	return s.service.SaveDesktopPrefs(ctx, in)
}

func (s appService) GetProxyStatus(ctx context.Context) (app.ProxyStatusView, error) {
	return s.service.GetProxyStatus(ctx)
}

func (s appService) GetProxySettings(ctx context.Context) (app.ProxySettingsView, error) {
	return s.service.GetProxySettings(ctx)
}

func (s appService) SaveProxySettings(ctx context.Context, in app.ProxySettingsInput) (app.ProxySettingsSaveResult, error) {
	return s.service.SaveProxySettings(ctx, in)
}

func (s appService) ListRequestTraces(ctx context.Context, limit int) ([]app.RequestTrace, error) {
	return s.service.ListRequestTraces(ctx, limit)
}

func (s appService) QueryRequestTraces(ctx context.Context, in app.RequestTraceListInput) (app.RequestTraceListResult, error) {
	return s.service.QueryRequestTraces(ctx, in)
}

func (s appService) GetRequestTrace(ctx context.Context, id uint64) (app.RequestTrace, error) {
	return s.service.GetRequestTrace(ctx, id)
}

func (s appService) StartProxy(ctx context.Context) (app.ProxyStatusView, error) {
	if err := s.service.StartProxy(ctx); err != nil {
		return app.ProxyStatusView{}, err
	}
	return s.service.GetProxyStatus(ctx)
}

func (s appService) StopProxy(ctx context.Context) (app.ProxyStatusView, error) {
	if err := s.service.StopProxy(ctx); err != nil {
		return app.ProxyStatusView{}, err
	}
	return s.service.GetProxyStatus(ctx)
}

func (s appService) RunDoctor(ctx context.Context) (app.DoctorReport, error) {
	return s.service.RunDoctor(ctx)
}

func (s appService) PreviewOpenCodeSync(ctx context.Context, in app.SyncInput) (app.SyncPreview, error) {
	return s.service.PreviewOpenCodeSyncWithBaseURL(ctx, in, s.publicBaseURL)
}

func (s appService) SyncOpenCode(ctx context.Context, in app.SyncInput) (app.SyncResult, error) {
	return s.service.ApplyOpenCodeSync(ctx, in)
}
