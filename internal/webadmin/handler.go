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
	GetProviderPriority(context.Context) (appcore.ProviderPriorityResult, error)
	SetProviderPriority(context.Context, appcore.ProviderPriorityInput) (appcore.ProviderPriorityResult, error)
	GetAutoAliasSettings(context.Context) (appcore.AutoAliasSettingsResult, error)
	SetAutoAliasSettings(context.Context, appcore.AutoAliasSettingsInput) (appcore.AutoAliasSettingsResult, error)
	ListAliases(context.Context) ([]appcore.AliasView, error)
	UpsertAlias(context.Context, appcore.AliasUpsertInput) (appcore.AliasView, error)
	RemoveAlias(context.Context, string) error
	BindAliasTarget(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	SetAliasTargetDisabled(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	UnbindAliasTarget(context.Context, appcore.AliasTargetInput) (appcore.AliasView, error)
	ReorderAliasTargets(context.Context, appcore.AliasTargetReorderInput) (appcore.AliasView, error)
	UpgradeAutoAlias(context.Context, appcore.AliasLockInput) (appcore.AliasView, error)
	ListRequestRewriteRules(context.Context) ([]appcore.RequestRewriteRuleView, error)
	UpsertRequestRewriteRule(context.Context, appcore.RequestRewriteRuleInput) (appcore.RequestRewriteRuleView, error)
	SetRequestRewriteRuleEnabled(context.Context, appcore.RequestRewriteRuleStateInput) (appcore.RequestRewriteRuleView, error)
	RemoveRequestRewriteRule(context.Context, appcore.RequestRewriteRuleRemoveInput) (appcore.RequestRewriteRuleRemoveResult, error)
	ReorderRequestRewriteRules(context.Context, appcore.RequestRewriteRuleReorderInput) (appcore.RequestRewriteRuleReorderResult, error)
	GetDesktopPrefs(context.Context) (appcore.DesktopPrefsView, error)
	SaveDesktopPrefs(context.Context, appcore.DesktopPrefsInput) (appcore.DesktopPrefsView, error)
	GetProxyStatus(context.Context) (appcore.ProxyStatusView, error)
	GetProxySettings(context.Context) (appcore.ProxySettingsView, error)
	SaveProxySettings(context.Context, appcore.ProxySettingsInput) (appcore.ProxySettingsSaveResult, error)
	ListRequestTraces(context.Context, int) ([]appcore.RequestTrace, error)
	QueryRequestTraces(context.Context, appcore.RequestTraceListInput) (appcore.RequestTraceListResult, error)
	QueryProviderHealth(context.Context, appcore.ProviderHealthInput) (appcore.ProviderHealthResult, error)
	GetRequestTrace(context.Context, uint64) (appcore.RequestTrace, error)
	StartProxy(context.Context) (appcore.ProxyStatusView, error)
	StopProxy(context.Context) (appcore.ProxyStatusView, error)
	RunDoctor(context.Context) (appcore.DoctorReport, error)
	PreviewOpenCodeSync(context.Context, appcore.SyncInput) (appcore.SyncPreview, error)
	PreviewOpenCodeSyncDiff(context.Context, appcore.SyncInput) (appcore.SyncPreview, error)
	SyncOpenCode(context.Context, appcore.SyncInput) (appcore.SyncResult, error)
	GetConfigRevision(context.Context) (appcore.ConfigRevision, error)
	PreviewLifecycle(context.Context, appcore.LifecyclePreviewInput) (appcore.LifecyclePlanView, error)
	ExecuteLifecycle(context.Context, appcore.LifecycleExecuteInput) (appcore.LifecycleExecuteResult, error)
}

type providerGroupService interface {
	ListProviderGroups(context.Context, string) ([]appcore.ProviderGroupView, error)
	CreateProviderGroup(context.Context, appcore.ProviderGroupCreateInput) (appcore.ProviderGroupView, error)
	UpdateProviderGroup(context.Context, appcore.ProviderGroupUpdateInput) (appcore.ProviderGroupView, error)
	DeleteProviderGroup(context.Context, appcore.ProviderGroupDeleteInput) error
	RefreshProviderGroupModels(context.Context, appcore.ProviderGroupRefreshModelsInput) (appcore.ProviderSaveResult, error)
	PingProviderGroupBaseURL(context.Context, appcore.ProviderGroupPingInput) (appcore.ProviderPingResult, error)
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

// apiEnvelope keeps legacy data/error fields and adds artifact-06 ok/outcome.
type apiEnvelope = appcore.APIEnvelope

type MetaView struct {
	Version      string       `json:"version"`
	Shell        string       `json:"shell"`
	URL          string       `json:"url,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

type Capabilities struct {
	DesktopPrefs             bool `json:"desktopPrefs"`
	OpenCodeDirectSync       bool `json:"openCodeDirectSync"`
	ProxyControl             bool `json:"proxyControl"`
	TransportEnvelopeVersion int  `json:"transportEnvelopeVersion"`
	LifecycleContractVersion int  `json:"lifecycleContractVersion"`
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
	groups, groupsAvailable := any(b).(providerGroupService)
	capabilities := Capabilities{
		DesktopPrefs:             !opts.ServerMode,
		OpenCodeDirectSync:       !opts.ServerMode,
		ProxyControl:             true,
		TransportEnvelopeVersion: 1,
		LifecycleContractVersion: 1,
	}

	api.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		writeSuccess(w, MetaView{Version: opts.Version, Shell: opts.Shell, URL: opts.BaseURL, Capabilities: capabilities})
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

	api.HandleFunc("/api/providers/priority", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.GetProviderPriority(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.ProviderPriorityInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.SetProviderPriority(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	// Provider Group management (upstream keys). Frozen under /api/admin/...;
	// never register /api-keys or reuse client proxy API-key handlers.
	api.HandleFunc("/api/admin/providers/{providerID}/groups", func(w http.ResponseWriter, r *http.Request) {
		if !groupsAvailable {
			writeInvalidRequest(w, "provider_group_management_unavailable")
			return
		}
		providerID := strings.TrimSpace(r.PathValue("providerID"))
		if providerID == "" {
			writeInvalidRequest(w, "provider_id_required")
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := groups.ListProviderGroups(r.Context(), providerID)
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.ProviderGroupCreateInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			// Path identity wins — body providerId cannot override the route.
			in.ProviderID = providerID
			data, err := groups.CreateProviderGroup(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/admin/providers/{providerID}/groups/{groupID}", func(w http.ResponseWriter, r *http.Request) {
		if !groupsAvailable {
			writeInvalidRequest(w, "provider_group_management_unavailable")
			return
		}
		providerID := strings.TrimSpace(r.PathValue("providerID"))
		groupID := strings.TrimSpace(r.PathValue("groupID"))
		if providerID == "" {
			writeInvalidRequest(w, "provider_id_required")
			return
		}
		if groupID == "" {
			writeInvalidRequest(w, "group_id_required")
			return
		}
		switch r.Method {
		case http.MethodPut:
			var in appcore.ProviderGroupUpdateInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			// Path supplies old identity; Group.ID may request a rename.
			in.ProviderID = providerID
			in.GroupID = groupID
			data, err := groups.UpdateProviderGroup(r.Context(), in)
			writeResult(w, data, err)
		case http.MethodDelete:
			var in appcore.ProviderGroupDeleteInput
			// Optional lifecycle selections body (empty body is valid).
			if !decodeJSONBody(w, r, &in) {
				return
			}
			in.ProviderID = providerID
			in.GroupID = groupID
			writeResult(w, map[string]bool{"ok": true}, groups.DeleteProviderGroup(r.Context(), in))
		default:
			writeMethodNotAllowed(w, http.MethodPut, http.MethodDelete)
		}
	})

	api.HandleFunc("/api/admin/providers/{providerID}/groups/{groupID}/refresh-models", func(w http.ResponseWriter, r *http.Request) {
		if !groupsAvailable {
			writeInvalidRequest(w, "provider_group_management_unavailable")
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		providerID := strings.TrimSpace(r.PathValue("providerID"))
		groupID := strings.TrimSpace(r.PathValue("groupID"))
		if providerID == "" {
			writeInvalidRequest(w, "provider_id_required")
			return
		}
		if groupID == "" {
			writeInvalidRequest(w, "group_id_required")
			return
		}
		var in appcore.ProviderGroupRefreshModelsInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		in.ProviderID = providerID
		in.GroupID = groupID
		data, err := groups.RefreshProviderGroupModels(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/admin/providers/{providerID}/groups/{groupID}/ping", func(w http.ResponseWriter, r *http.Request) {
		if !groupsAvailable {
			writeInvalidRequest(w, "provider_group_management_unavailable")
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		providerID := strings.TrimSpace(r.PathValue("providerID"))
		groupID := strings.TrimSpace(r.PathValue("groupID"))
		if providerID == "" {
			writeInvalidRequest(w, "provider_id_required")
			return
		}
		if groupID == "" {
			writeInvalidRequest(w, "group_id_required")
			return
		}
		var in appcore.ProviderGroupPingInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		// Path identity wins over any body providerId/groupId.
		in.ProviderID = providerID
		in.GroupID = groupID
		data, err := groups.PingProviderGroupBaseURL(r.Context(), in)
		writeResult(w, data, err)
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

	api.HandleFunc("/api/auto-alias-settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.GetAutoAliasSettings(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.AutoAliasSettingsInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.SetAutoAliasSettings(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/aliases/upgrade-manual", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.AliasLockInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.UpgradeAutoAlias(r.Context(), in)
		writeResult(w, data, err)
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

	api.HandleFunc("/api/rewrite-rules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.ListRequestRewriteRules(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.RequestRewriteRuleInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := b.UpsertRequestRewriteRule(r.Context(), in)
			writeResult(w, data, err)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	api.HandleFunc("/api/rewrite-rules/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.RequestRewriteRuleStateInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.SetRequestRewriteRuleEnabled(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/rewrite-rules/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.RequestRewriteRuleRemoveInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.RemoveRequestRewriteRule(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/rewrite-rules/reorder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.RequestRewriteRuleReorderInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.ReorderRequestRewriteRules(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/desktop-prefs", func(w http.ResponseWriter, r *http.Request) {
		if opts.ServerMode {
			writeOutcome(w, http.StatusNotFound, apiEnvelope{
				OK:    false,
				Error: "not_found",
				Outcome: appcore.TransportOutcome{
					Code:      "not_found",
					Params:    map[string]any{"resourceType": "desktop_prefs"},
					Retryable: false,
				},
			})
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

	api.HandleFunc("/api/proxy/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.ProviderHealthInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.QueryProviderHealth(r.Context(), in)
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
		// Doctor keeps soft-error payload shape for UI continuity.
		writeSuccess(w, appcore.DoctorRunResult{Report: data, Error: errorString(err)})
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

	api.HandleFunc("/api/opencode-sync/preview-diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.SyncInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.PreviewOpenCodeSyncDiff(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/opencode-sync/apply", func(w http.ResponseWriter, r *http.Request) {
		if opts.ServerMode {
			writeOutcome(w, http.StatusForbidden, apiEnvelope{
				OK:    false,
				Error: "forbidden",
				Outcome: appcore.TransportOutcome{
					Code:      "forbidden",
					Params:    map[string]any{"reason": "opencode_direct_sync_disabled"},
					Retryable: false,
				},
			})
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

	api.HandleFunc("/api/config/revision", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.GetConfigRevision(r.Context())
		writeResult(w, map[string]appcore.ConfigRevision{"revision": data}, err)
	})

	api.HandleFunc("/api/lifecycle/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.LifecyclePreviewInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.PreviewLifecycle(r.Context(), in)
		writeResult(w, data, err)
	})

	api.HandleFunc("/api/lifecycle/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		var in appcore.LifecycleExecuteInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		data, err := b.ExecuteLifecycle(r.Context(), in)
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
		writeOutcome(w, http.StatusBadRequest, apiEnvelope{
			OK:    false,
			Error: "invalid_request",
			Outcome: appcore.TransportOutcome{
				Code:      "invalid_request",
				Params:    map[string]any{"reason": "body_read_failed"},
				Retryable: false,
			},
		})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeOutcome(w, http.StatusBadRequest, apiEnvelope{
			OK:    false,
			Error: "invalid_request",
			Outcome: appcore.TransportOutcome{
				Code:      "invalid_request",
				Params:    map[string]any{"reason": "invalid_json"},
				Retryable: false,
			},
		})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, data any, err error) {
	status, env := appcore.ClassifyOutcome(err, data)
	writeOutcome(w, status, env)
}

func writeSuccess(w http.ResponseWriter, data any) {
	writeResult(w, data, nil)
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeOutcome(w, http.StatusMethodNotAllowed, apiEnvelope{
		OK:    false,
		Error: "method_not_allowed",
		Outcome: appcore.TransportOutcome{
			Code:      "method_not_allowed",
			Params:    map[string]any{},
			Retryable: false,
		},
	})
}

func writeInvalidRequest(w http.ResponseWriter, reason string) {
	writeOutcome(w, http.StatusBadRequest, apiEnvelope{
		OK:    false,
		Error: "invalid_request",
		Outcome: appcore.TransportOutcome{
			Code:      "invalid_request",
			Params:    map[string]any{"reason": reason},
			Retryable: false,
		},
	})
}

func writeOutcome(w http.ResponseWriter, status int, v apiEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusServiceUnavailable && v.Outcome.Code == "config_store_busy" {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
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
