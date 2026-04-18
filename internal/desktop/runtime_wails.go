//go:build desktop_wails

package desktop

import (
	"context"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type desktopNotification struct {
	ID    string
	Title string
	Body  string
}

func hideWindow(ctx context.Context) error {
	wruntime.Hide(ctx)
	return nil
}

func showWindow(ctx context.Context) error {
	wruntime.Show(ctx)
	return nil
}

func quitWindow(ctx context.Context) error {
	wruntime.Quit(ctx)
	return nil
}

func initDesktopNotifications(ctx context.Context) error {
	return wruntime.InitializeNotifications(ctx)
}

func desktopNotificationsAvailable(ctx context.Context) bool {
	return wruntime.IsNotificationAvailable(ctx)
}

func sendDesktopNotification(ctx context.Context, notification desktopNotification) error {
	return wruntime.SendNotification(ctx, wruntime.NotificationOptions{
		ID:    notification.ID,
		Title: notification.Title,
		Body:  notification.Body,
	})
}
