//go:build !desktop_wails

package desktop

import (
	"context"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

// Tray is a no-op shell adapter outside the Wails desktop build.
type Tray struct {
	service *app.Service
	ctx     context.Context
	prefs   app.DesktopPrefsView
}

func NewTray(service *app.Service) *Tray {
	return &Tray{service: service}
}

func (t *Tray) Attach(ctx context.Context) {
	t.ctx = ctx
}

func (t *Tray) Detach() {
	t.ctx = nil
}

func (t *Tray) Sync(ctx context.Context, prefs app.DesktopPrefsView) {
	_ = ctx
	t.prefs = prefs
}

func (t *Tray) RefreshProxyStatus(ctx context.Context) {
	_ = ctx
}

func (t *Tray) BeforeClose(ctx context.Context) (bool, error) {
	_ = ctx
	if !t.prefs.MinimizeToTray {
		return false, nil
	}
	return true, hideWindow(ctx)
}
