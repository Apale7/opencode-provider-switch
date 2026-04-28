package webadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	frontendassets "github.com/Apale7/opencode-provider-switch/frontend"
	appcore "github.com/Apale7/opencode-provider-switch/internal/app"
)

type Service interface {
	GetOverview(context.Context) (appcore.Overview, error)
	ExportConfig(context.Context) (appcore.ConfigExportView, error)
	ImportConfig(context.Context, appcore.ConfigImportInput) (appcore.ConfigImportResult, error)
	ListProviders(context.Context) ([]appcore.ProviderView, error)
	UpsertProvider(context.Context, appcore.ProviderUpsertInput) (appcore.ProviderSaveResult, error)
	ImportProviders(context.Context, appcore.ProviderImportInput) (appcore.ProviderImportResult, error)
	RefreshProviderModels(context.Context, appcore.ProviderRefreshModelsInput) (appcore.ProviderSaveResult, error)
	PingProviderBaseURL(context.Context, appcore.ProviderPingInput) (appcore.ProviderPingResult, error)
	SetProviderDisabled(context.Context, appcore.ProviderStateInput) (appcore.ProviderView, error)
	RemoveProvider(context.Context, string) error
	ListAliases(context.Context) ([]appcore.AliasView, error)
	UpsertAlias(context.Context, appcore.AliasUpsertInput) (appcore.AliasView, error)
	RemoveAlias(context.Context, string) error
	BindAliasTarget(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	SetAliasTargetDisabled(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	UnbindAliasTarget(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	ReorderAliasTargets(context.Context, appcore.AliasTargetReorderInput) (appcore.AliasView, error)
	GetDesktopPrefs(context.Context) (appcore.DesktopPrefsView, error)
	SaveDesktopPrefs(context.Context, appcore.DesktopPrefsInput) (appcore.DesktopPrefsView, error)
	GetProxyStatus(context.Context) (appcore.ProxyStatusView, error)
	GetProxySettings(context.Context) (appcore.ProxySettingsView, error)
	SaveProxySettings(context.Context, appcore.ProxySettingsInput) (appcore.ProxySettingsSaveResult, error)
	ListRequestTraces(context.Context, int) ([]appcore.RequestTrace, error)
	QueryRequestTraces(context.Context, appcore.RequestTraceListInput) (appcore.RequestTraceListResult, error)
	GetRequestTrace(context.Context, uint64) (appcore.RequestTrace, error)
	StartProxy(context.Context) (appcore.ProxyStatusView, error)
	StopProxy(context.Context) (appcore.ProxyStatusView, error)
	RunDoctor(context.Context) (appcore.DoctorReport, error)
	PreviewOpenCodeSync(context.Context, appcore.SyncInput) (appcore.SyncPreview, error)
	SyncOpenCode(context.Context, appcore.SyncInput) (appcore.SyncResult, error)
}

type ImportConfigFunc func(context.Context, appcore.ConfigImportInput) (appcore.ConfigImportResult, error)
type SaveDesktopPrefsFunc func(context.Context, appcore.DesktopPrefsInput) (appcore.DesktopPrefsSaveResult, error)

type Options struct {
	Version          string
	Shell            string
	BaseURL          string
	Service          Service
	ImportConfig     ImportConfigFunc
	SaveDesktopPrefs SaveDesktopPrefsFunc
	Auth             func(http.ResponseWriter, *http.Request) bool
	SecureHeaders    bool
	ServerMode       bool
}

type apiEnvelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type MetaView struct {
	Version      string       `json:"version"`
	Shell        string       `json:"shell"`
	URL          string       `json:"url,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

type Capabilities struct {
	DesktopPrefs       bool `json:"desktopPrefs"`
	OpenCodeDirectSync bool `json:"openCodeDirectSync"`
	ProxyControl       bool `json:"proxyControl"`
}

func NewHandler(opts Options) (http.Handler, error) {
	if opts.Service == nil {
		return nil, fmt.Errorf("webadmin service is required")
	}
	if strings.TrimSpace(opts.Shell) == "" {
		opts.Shell = "browser"
	}
	assets, err := frontendassets.DistFS()
	if err != nil {
		return nil, fmt.Errorf("load web assets: %w", err)
	}

	api := http.NewServeMux()
	b := opts.Service
	capabilities := Capabilities{
		DesktopPrefs:       !opts.ServerMode,
		OpenCodeDirectSync: !opts.ServerMode,
		ProxyControl:       true,
	}

	api.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, apiEnvelope{Data: MetaView{Version: opts.Version, Shell: opts.Shell, URL: opts.BaseURL, Capabilities: capabilities}})
	})

	api.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.GetOverview(r.Context())
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/config/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.ExportConfig(r.Context())
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/config/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ConfigImportInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		if opts.ImportConfig != nil {
			data, err := opts.ImportConfig(r.Context(), in)
			writeResult(w, data, err)
			return
		}
		data, err := b.ImportConfig(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.ListProviders(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.ProviderUpsertInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.UpsertProvider(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/providers/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ProviderImportInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.ImportProviders(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/providers/refresh-models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ProviderRefreshModelsInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.RefreshProviderModels(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/providers/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ProviderPingInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.PingProviderBaseURL(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/providers/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ProviderStateInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.SetProviderDisabled(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/providers/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var payload struct {
			ID string `json:"id"`
		}
		if !decodeJSONBody(w, r, &payload) {
			return
		}
		writeResult(w, map[string]bool{"ok": true}, b.RemoveProvider(r.Context(), payload.ID))
	})

	api.HandleFunc("/api/aliases", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.ListAliases(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.AliasUpsertInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.UpsertAlias(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/aliases/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var payload struct {
			Alias string `json:"alias"`
		}
		if !decodeJSONBody(w, r, &payload) {
			return
		}
		writeResult(w, map[string]bool{"ok": true}, b.RemoveAlias(r.Context(), payload.Alias))
	})

	api.HandleFunc("/api/aliases/bind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.AliasTargetInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.BindAliasTarget(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/aliases/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.AliasTargetInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.SetAliasTargetDisabled(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/aliases/unbind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.AliasTargetInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.UnbindAliasTarget(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/aliases/reorder-targets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.AliasTargetReorderInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.ReorderAliasTargets(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/desktop-prefs", func(w http.ResponseWriter, r *http.Request) {
		if opts.ServerMode {
			writeJSON(w, http.StatusNotFound, apiEnvelope{Error: "desktop preferences are not available in server mode"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := b.GetDesktopPrefs(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.DesktopPrefsInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			if opts.SaveDesktopPrefs != nil {
				data, err := opts.SaveDesktopPrefs(r.Context(), in)
				writeResult(w, data, err)
				return
			}
			prefs, err := b.SaveDesktopPrefs(r.Context(), in)
			writeResult(w, appcore.DesktopPrefsSaveResult{Prefs: prefs}, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/proxy/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.GetProxyStatus(r.Context())
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/proxy/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.GetProxySettings(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.ProxySettingsInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.SaveProxySettings(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/proxy/traces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.ListRequestTraces(r.Context(), 100)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/proxy/traces/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.RequestTraceListInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.QueryRequestTraces(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/proxy/traces/detail", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var payload appcore.RequestTraceDetailInput
		if !decodeJSONBody(w, r, &payload) {
			return
		}
		data, err := b.GetRequestTrace(r.Context(), payload.ID)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/proxy/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		data, err := b.StartProxy(r.Context())
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/proxy/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		data, err := b.StopProxy(ctx)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/doctor", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		data, err := b.RunDoctor(r.Context())
		writeJSON(w, http.StatusOK, apiEnvelope{Data: appcore.DoctorRunResult{Report: data, Error: errorString(err)}})
	})

	api.HandleFunc("/api/opencode-sync/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.SyncInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.PreviewOpenCodeSync(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/opencode-sync/apply", func(w http.ResponseWriter, r *http.Request) {
		if opts.ServerMode {
			writeJSON(w, http.StatusForbidden, apiEnvelope{Error: "direct OpenCode sync is disabled in server mode; copy the generated config instead"})
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.SyncInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.SyncOpenCode(r.Context(), in)
		writeResult(w, data, err)
	})

	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.SecureHeaders {
			setSecurityHeaders(w)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if opts.Auth != nil && !opts.Auth(w, r) {
				return
			}
			api.ServeHTTP(w, r)
			return
		}
		serveSPA(w, r, assets, fileServer)
	}), nil
}

func serveSPA(w http.ResponseWriter, r *http.Request, assets fs.FS, next http.Handler) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(assets, path); err == nil {
		next.ServeHTTP(w, r)
		return
	}
	r = r.Clone(r.Context())
	r.URL.Path = "/index.html"
	next.ServeHTTP(w, r)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiEnvelope{Error: err.Error()})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, apiEnvelope{Error: "invalid json: " + err.Error()})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiEnvelope{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apiEnvelope{Data: data})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeJSON(w, http.StatusMethodNotAllowed, apiEnvelope{Error: "method not allowed"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
}
