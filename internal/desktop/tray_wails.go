//go:build desktop_wails

package desktop

import (
	"context"
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	frontendassets "github.com/Apale7/opencode-provider-switch/frontend"
	"github.com/Apale7/opencode-provider-switch/internal/app"
)

//go:embed assets/icon.ico
var trayIcon []byte

// Tray wires resident-mode controls into a native system tray.
type Tray struct {
	service *app.Service

	mu         sync.Mutex
	ctx        context.Context
	prefs      app.DesktopPrefsView
	language   string
	registered bool
	ready      bool
	running    bool
	quitting   bool

	statusItem *systray.MenuItem
	showItem   *systray.MenuItem
	hideItem   *systray.MenuItem
	startItem  *systray.MenuItem
	stopItem   *systray.MenuItem
	quitItem   *systray.MenuItem
}

func NewTray(service *app.Service) *Tray {
	return &Tray{service: service}
}

func (t *Tray) Attach(ctx context.Context) {
	t.mu.Lock()
	t.ctx = ctx
	if t.registered {
		t.mu.Unlock()
		return
	}
	t.registered = true
	t.mu.Unlock()

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(t.onReady, t.onExit)
	}()
}

func (t *Tray) Detach() {
	t.mu.Lock()
	t.ctx = nil
	registered := t.registered
	t.mu.Unlock()
	if registered {
		systray.Quit()
	}
}

func (t *Tray) Sync(ctx context.Context, prefs app.DesktopPrefsView) {
	t.mu.Lock()
	if ctx != nil {
		t.ctx = ctx
	}
	t.prefs = prefs
	t.language = trayLanguage(prefs.Language)
	ready := t.ready
	statusItem := t.statusItem
	showItem := t.showItem
	hideItem := t.hideItem
	startItem := t.startItem
	stopItem := t.stopItem
	quitItem := t.quitItem
	labels := t.trayLabelsLocked()
	t.mu.Unlock()
	if ready && statusItem != nil && showItem != nil && hideItem != nil && startItem != nil && stopItem != nil && quitItem != nil {
		systray.SetTitle(labels.appTitle)
		systray.SetTooltip(labels.appTooltip)
		showItem.SetTitle(labels.openWindow)
		showItem.SetTooltip(labels.openWindowHint)
		hideItem.SetTitle(labels.hideWindow)
		hideItem.SetTooltip(labels.hideWindowHint)
		startItem.SetTitle(labels.startProxy)
		startItem.SetTooltip(labels.startProxyHint)
		stopItem.SetTitle(labels.stopProxy)
		stopItem.SetTooltip(labels.stopProxyHint)
		quitItem.SetTitle(labels.quit)
		quitItem.SetTooltip(labels.quitHint)
	}
	t.refresh()
}

func (t *Tray) RefreshProxyStatus(ctx context.Context) {
	t.mu.Lock()
	if ctx != nil {
		t.ctx = ctx
	}
	t.mu.Unlock()
	t.refresh()
}

func (t *Tray) BeforeClose(ctx context.Context) (bool, error) {
	t.mu.Lock()
	t.ctx = ctx
	quitting := t.quitting
	minimize := t.prefs.MinimizeToTray
	t.mu.Unlock()
	if quitting || !minimize {
		return false, nil
	}
	return true, hideWindow(ctx)
}

func (t *Tray) onReady() {
	labels := t.labels()
	if len(trayIcon) > 0 {
		systray.SetIcon(trayIcon)
	}
	systray.SetTitle(labels.appTitle)
	systray.SetTooltip(labels.appTooltip)
	systray.SetOnTapped(func() {
		_ = t.withContext(showWindow)
	})

	t.mu.Lock()
	t.ready = true
	t.running = true
	t.statusItem = systray.AddMenuItem(labels.proxyChecking, labels.proxyStatusHint)
	t.statusItem.Disable()
	systray.AddSeparator()
	t.showItem = systray.AddMenuItem(labels.openWindow, labels.openWindowHint)
	t.hideItem = systray.AddMenuItem(labels.hideWindow, labels.hideWindowHint)
	systray.AddSeparator()
	t.startItem = systray.AddMenuItem(labels.startProxy, labels.startProxyHint)
	t.stopItem = systray.AddMenuItem(labels.stopProxy, labels.stopProxyHint)
	systray.AddSeparator()
	t.quitItem = systray.AddMenuItem(labels.quit, labels.quitHint)
	t.mu.Unlock()

	go t.loop()
	t.refresh()
}

func (t *Tray) onExit() {
	t.mu.Lock()
	t.ready = false
	t.running = false
	t.registered = false
	t.statusItem = nil
	t.showItem = nil
	t.hideItem = nil
	t.startItem = nil
	t.stopItem = nil
	t.quitItem = nil
	t.mu.Unlock()
}

func (t *Tray) loop() {
	for {
		t.mu.Lock()
		showItem := t.showItem
		hideItem := t.hideItem
		startItem := t.startItem
		stopItem := t.stopItem
		quitItem := t.quitItem
		running := t.running
		t.mu.Unlock()
		if !running || showItem == nil || hideItem == nil || startItem == nil || stopItem == nil || quitItem == nil {
			return
		}

		select {
		case <-showItem.ClickedCh:
			_ = t.withContext(showWindow)
		case <-hideItem.ClickedCh:
			_ = t.withContext(hideWindow)
		case <-startItem.ClickedCh:
			t.startProxy()
		case <-stopItem.ClickedCh:
			t.stopProxy()
		case <-quitItem.ClickedCh:
			t.requestQuit()
			return
		}
	}
}

func (t *Tray) startProxy() {
	if t.service == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = t.service.StartProxy(ctx)
	t.refresh()
}

func (t *Tray) stopProxy() {
	if t.service == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = t.service.StopProxy(ctx)
	t.refresh()
}

func (t *Tray) requestQuit() {
	t.mu.Lock()
	t.quitting = true
	ctx := t.ctx
	t.mu.Unlock()
	if ctx == nil {
		return
	}
	_ = quitWindow(ctx)
}

func (t *Tray) withContext(fn func(context.Context) error) error {
	t.mu.Lock()
	ctx := t.ctx
	t.mu.Unlock()
	if ctx == nil {
		return nil
	}
	return fn(ctx)
}

func (t *Tray) refresh() {
	t.mu.Lock()
	ready := t.ready
	statusItem := t.statusItem
	startItem := t.startItem
	stopItem := t.stopItem
	labels := t.trayLabelsLocked()
	t.mu.Unlock()
	if !ready || statusItem == nil || startItem == nil || stopItem == nil || t.service == nil {
		return
	}

	status, err := t.service.GetProxyStatus(context.Background())
	if err != nil {
		statusItem.SetTitle(labels.proxyUnavailable)
		startItem.Enable()
		stopItem.Disable()
		return
	}

	if status.Running {
		statusItem.SetTitle(fmt.Sprintf(labels.proxyRunning, status.BindAddress))
		startItem.Disable()
		stopItem.Enable()
		return
	}

	statusItem.SetTitle(fmt.Sprintf(labels.proxyStopped, status.BindAddress))
	startItem.Enable()
	stopItem.Disable()
}

func (t *Tray) labels() trayLabels {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trayLabelsLocked()
}

func (t *Tray) trayLabelsLocked() trayLabels {
	return trayLabels{
		appTitle:         trayLocaleString(t.language, "tray", "appTitle", "ocswitch"),
		appTooltip:       trayLocaleString(t.language, "tray", "appTooltip", "ocswitch desktop"),
		proxyChecking:    trayLocaleString(t.language, "tray", "proxyChecking", "Proxy: checking..."),
		proxyStatusHint:  trayLocaleString(t.language, "tray", "proxyStatusHint", "Current proxy status"),
		proxyUnavailable: trayLocaleString(t.language, "tray", "proxyUnavailable", "Proxy: unavailable"),
		proxyRunning:     trayLocaleString(t.language, "tray", "proxyRunning", "Proxy: running (%s)"),
		proxyStopped:     trayLocaleString(t.language, "tray", "proxyStopped", "Proxy: stopped (%s)"),
		openWindow:       trayLocaleString(t.language, "tray", "openWindow", "Open window"),
		openWindowHint:   trayLocaleString(t.language, "tray", "openWindowHint", "Show desktop window"),
		hideWindow:       trayLocaleString(t.language, "tray", "hideWindow", "Hide window"),
		hideWindowHint:   trayLocaleString(t.language, "tray", "hideWindowHint", "Hide desktop window"),
		startProxy:       trayLocaleString(t.language, "tray", "startProxy", "Start proxy"),
		startProxyHint:   trayLocaleString(t.language, "tray", "startProxyHint", "Start local proxy"),
		stopProxy:        trayLocaleString(t.language, "tray", "stopProxy", "Stop proxy"),
		stopProxyHint:    trayLocaleString(t.language, "tray", "stopProxyHint", "Stop local proxy"),
		quit:             trayLocaleString(t.language, "tray", "quit", "Quit"),
		quitHint:         trayLocaleString(t.language, "tray", "quitHint", "Exit application"),
	}
}

func trayLocaleString(language string, key string, field string, fallback string) string {
	value, ok, err := frontendassets.LocaleValue(language, key, field)
	if err == nil && ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

type trayLabels struct {
	appTitle         string
	appTooltip       string
	proxyChecking    string
	proxyStatusHint  string
	proxyUnavailable string
	proxyRunning     string
	proxyStopped     string
	openWindow       string
	openWindowHint   string
	hideWindow       string
	hideWindowHint   string
	startProxy       string
	startProxyHint   string
	stopProxy        string
	stopProxyHint    string
	quit             string
	quitHint         string
}
