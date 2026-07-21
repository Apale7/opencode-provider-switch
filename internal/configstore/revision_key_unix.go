//go:build !windows

package configstore

import "fmt"

func protectRevisionKey(key []byte) ([]byte, error) {
	if len(key) != revisionKeySize {
		return nil, fmt.Errorf("invalid key size %d", len(key))
	}
	return append([]byte(nil), key...), nil
}

func unprotectRevisionKey(stored []byte) ([]byte, error) {
	if len(stored) != revisionKeySize {
		return nil, fmt.Errorf("invalid stored size %d", len(stored))
	}
	return append([]byte(nil), stored...), nil
}
