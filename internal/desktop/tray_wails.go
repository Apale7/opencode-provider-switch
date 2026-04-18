//go:build desktop_wails

package desktop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/getlantern/systray"
)

// Tray wires resident-mode controls into a native system tray.
type Tray struct {
	service *app.Service

	mu         sync.Mutex
	ctx        context.Context
	prefs      app.DesktopPrefsView
	registered bool
	ready      bool
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
	systray.Register(t.onReady, nil)
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
	t.mu.Unlock()
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
	systray.SetTitle("ocswitch")
	systray.SetTooltip("ocswitch desktop")

	t.mu.Lock()
	t.ready = true
	t.statusItem = systray.AddMenuItem("Proxy: checking...", "Current proxy status")
	t.statusItem.Disable()
	systray.AddSeparator()
	t.showItem = systray.AddMenuItem("Open window", "Show desktop window")
	t.hideItem = systray.AddMenuItem("Hide window", "Hide desktop window")
	systray.AddSeparator()
	t.startItem = systray.AddMenuItem("Start proxy", "Start local proxy")
	t.stopItem = systray.AddMenuItem("Stop proxy", "Stop local proxy")
	systray.AddSeparator()
	t.quitItem = systray.AddMenuItem("Quit", "Exit application")
	t.mu.Unlock()

	go t.loop()
	t.refresh()
}

func (t *Tray) loop() {
	for {
		t.mu.Lock()
		showItem := t.showItem
		hideItem := t.hideItem
		startItem := t.startItem
		stopItem := t.stopItem
		quitItem := t.quitItem
		t.mu.Unlock()

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
	t.mu.Unlock()
	if !ready || statusItem == nil || startItem == nil || stopItem == nil || t.service == nil {
		return
	}

	status, err := t.service.GetProxyStatus(context.Background())
	if err != nil {
		statusItem.SetTitle("Proxy: unavailable")
		startItem.Enable()
		stopItem.Disable()
		return
	}

	if status.Running {
		statusItem.SetTitle(fmt.Sprintf("Proxy: running (%s)", status.BindAddress))
		startItem.Disable()
		stopItem.Enable()
		return
	}

	statusItem.SetTitle(fmt.Sprintf("Proxy: stopped (%s)", status.BindAddress))
	startItem.Enable()
	stopItem.Disable()
}
