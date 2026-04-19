//go:build desktop_wails && windows

package desktop

import (
	"syscall"
	"unsafe"
)

func detectSystemTrayLanguage() string {
	const localeNameMaxLength = 85
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultLocaleName")
	var buffer [localeNameMaxLength]uint16
	result, _, _ := proc.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if result == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:])
}
