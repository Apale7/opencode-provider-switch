package desktop

import (
	"context"
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

	url := "http://" + listener.Addr().String()
	handler, err := webadmin.NewHandler(webadmin.Options{
		Version:          opts.Version,
		Shell:            instance.shellName(),
		BaseURL:          url,
		Service:          instance.Bindings(),
		SaveDesktopPrefs: instance.SaveDesktopPrefs,
	})
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
	return webadmin.NewHandler(webadmin.Options{
		Version:          version,
		Shell:            instance.shellName(),
		BaseURL:          baseURL,
		Service:          instance.Bindings(),
		ImportConfig:     instance.ImportConfigHTTP,
		SaveDesktopPrefs: instance.SaveDesktopPrefs,
	})
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
