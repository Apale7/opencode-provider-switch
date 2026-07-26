package desktop

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/webadmin"
)

type RunOptions struct {
	ConfigPath   string
	Version      string
	ListenAddr   string
	OpenBrowser  bool
	ShutdownWait time.Duration
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
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || !tcpAddr.IP.IsLoopback() {
		return fmt.Errorf("desktop control panel requires a loopback listen address")
	}
	token, err := generateDesktopSessionToken()
	if err != nil {
		return err
	}

	url := "http://" + listener.Addr().String()
	handler, err := newHandler(instance, opts.Version, url, token)
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
	fmt.Printf("one-time desktop session token: %s\n", token)
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

func newHandler(instance *App, version string, baseURL string, token string) (http.Handler, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("desktop session token is required")
	}
	return webadmin.NewHandler(webadmin.Options{
		Version:          version,
		Shell:            instance.shellName(),
		BaseURL:          baseURL,
		Service:          instance.Bindings(),
		ImportConfig:     instance.ImportConfigHTTP,
		SaveDesktopPrefs: instance.SaveDesktopPrefs,
		Auth:             desktopSessionAuth(token),
		SecureHeaders:    true,
	})
}

func generateDesktopSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate desktop session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func desktopSessionAuth(expected string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		got := ""
		if strings.HasPrefix(header, "Bearer ") {
			got = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		}
		if len(got) == len(expected) && subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1 {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="ocswitch-desktop"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
		return false
	}
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
	return errors.New(strings.Join(errs, "; "))
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
