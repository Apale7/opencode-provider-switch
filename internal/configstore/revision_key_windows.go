//go:build windows

package configstore

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectRevisionKey(key []byte) ([]byte, error) {
	if len(key) != revisionKeySize {
		return nil, fmt.Errorf("invalid key size %d", len(key))
	}
	in := windows.DataBlob{Size: uint32(len(key)), Data: &key[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotectRevisionKey(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, fmt.Errorf("empty protected key")
	}
	in := windows.DataBlob{Size: uint32(len(stored)), Data: &stored[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
