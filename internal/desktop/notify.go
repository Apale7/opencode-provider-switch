package desktop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

// Notifier bridges desktop preference state to native notifications.
type Notifier struct {
	service *app.Service

	mu          sync.Mutex
	ctx         context.Context
	prefs       app.DesktopPrefsView
	initialized bool
}

func NewNotifier(service *app.Service) *Notifier {
	return &Notifier{service: service}
}

func (n *Notifier) Attach(ctx context.Context) {
	n.mu.Lock()
	n.ctx = ctx
	n.mu.Unlock()
	_ = n.ensureInitialized(ctx)
}

func (n *Notifier) Detach() {
	n.mu.Lock()
	n.ctx = nil
	n.initialized = false
	n.mu.Unlock()
}

func (n *Notifier) Sync(ctx context.Context, prefs app.DesktopPrefsView) {
	n.mu.Lock()
	if ctx != nil {
		n.ctx = ctx
	}
	n.prefs = prefs
	n.mu.Unlock()
	if prefs.Notifications {
		_ = n.ensureInitialized(ctx)
	}
}

func (n *Notifier) Send(ctx context.Context, title string, body string) error {
	n.mu.Lock()
	if !n.prefs.Notifications {
		n.mu.Unlock()
		return nil
	}
	if ctx == nil {
		ctx = n.ctx
	}
	n.mu.Unlock()
	if ctx == nil {
		return nil
	}
	if err := n.ensureInitialized(ctx); err != nil {
		return err
	}
	if !desktopNotificationsAvailable(ctx) {
		return nil
	}
	return sendDesktopNotification(ctx, desktopNotification{
		ID:    fmt.Sprintf("ocswitch-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	})
}

func (n *Notifier) ensureInitialized(ctx context.Context) error {
	if ctx == nil {
		n.mu.Lock()
		ctx = n.ctx
		n.mu.Unlock()
	}
	if ctx == nil {
		return nil
	}

	n.mu.Lock()
	if n.initialized {
		n.mu.Unlock()
		return nil
	}
	n.mu.Unlock()

	if err := initDesktopNotifications(ctx); err != nil {
		return err
	}

	n.mu.Lock()
	n.initialized = true
	n.mu.Unlock()
	return nil
}
