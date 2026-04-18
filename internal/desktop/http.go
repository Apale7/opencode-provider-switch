package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	frontendassets "github.com/Apale7/opencode-provider-switch/frontend"
	appcore "github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
)

type RunOptions struct {
	ConfigPath   string
	Version      string
	ListenAddr   string
	OpenBrowser  bool
	ShutdownWait time.Duration
}

type apiEnvelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type metaView struct {
	Version string `json:"version"`
	Shell   string `json:"shell"`
	URL     string `json:"url"`
}

func Run(opts RunOptions) error {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = config.DefaultPath()
	}
	if strings.TrimSpace(opts.ListenAddr) == "" {
		opts.ListenAddr = "127.0.0.1:0"
	}
	if opts.ShutdownWait <= 0 {
		opts.ShutdownWait = 5 * time.Second
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	instance := New(opts.ConfigPath)
	listener, err := net.Listen("tcp", opts.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen desktop control panel: %w", err)
	}
	defer listener.Close()

	url := "http://" + listener.Addr().String()
	handler, err := newHandler(instance, opts.Version, url)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	fmt.Printf("ocswitch desktop control panel: %s\n", url)
	if opts.OpenBrowser {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "warning: open browser: %v\n", err)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownWait)
		defer cancel()
		_ = instance.Service().StopProxy(shutdownCtx)
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newHandler(instance *App, version string, baseURL string) (http.Handler, error) {
	assets, err := frontendassets.DistFS()
	if err != nil {
		return nil, fmt.Errorf("load web assets: %w", err)
	}
	api := http.NewServeMux()
	b := instance.Bindings()

	api.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, apiEnvelope{Data: metaView{Version: version, Shell: instance.shellName(), URL: baseURL}})
	})

	api.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		data, err := b.GetOverview(r.Context())
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

	api.HandleFunc("/api/desktop-prefs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := b.GetDesktopPrefs(r.Context())
			writeResult(w, data, err)
		case http.MethodPost:
			var in appcore.DesktopPrefsInput
			if !decodeJSONBody(w, r, &in) {
				return
			}
			data, err := instance.SaveDesktopPrefs(r.Context(), in)
			writeResult(w, data, err)
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
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

func openBrowser(url string) error {
	commands := browserCommands(url)
	var errs []string
	for _, args := range commands {
		if _, err := exec.LookPath(args[0]); err != nil {
			err = nil
			continue
		}
		if err := exec.Command(args[0], args[1:]...).Start(); err == nil {
			return nil
		} else {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) == 0 {
		return fmt.Errorf("no browser launcher found")
	}
	return fmt.Errorf(strings.Join(errs, "; "))
}

func browserCommands(url string) [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"open", url}}
	case "windows":
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		return [][]string{{"xdg-open", url}, {"gio", "open", url}}
	}
}
