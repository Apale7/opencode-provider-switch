//go:build !desktop_wails

package desktop

import "context"

type desktopNotification struct {
	ID    string
	Title string
	Body  string
}

func hideWindow(ctx context.Context) error {
	_ = ctx
	return nil
}

func showWindow(ctx context.Context) error {
	_ = ctx
	return nil
}

func quitWindow(ctx context.Context) error {
	_ = ctx
	return nil
}

func initDesktopNotifications(ctx context.Context) error {
	_ = ctx
	return nil
}

func desktopNotificationsAvailable(ctx context.Context) bool {
	_ = ctx
	return false
}

func sendDesktopNotification(ctx context.Context, notification desktopNotification) error {
	_ = ctx
	_ = notification
	return nil
}
