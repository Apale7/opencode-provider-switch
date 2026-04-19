//go:build desktop_wails && !windows

package desktop

func detectSystemTrayLanguage() string {
	return ""
}
