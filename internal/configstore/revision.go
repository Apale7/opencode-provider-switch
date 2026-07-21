package configstore

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/fileutil"
)

const (
	revisionKeySuffix = ".revision-key"
	revisionDomain    = "ocswitch/config-revision/v1"
	revisionKeySize   = 32
	revisionPrefix    = "v1."
)

const revisionKeyMode os.FileMode = 0o600

type Revision string

var ErrRevisionKeyInvalid = errors.New("config revision key is invalid")

func revisionFor(key []byte, path string, exists bool, raw []byte) Revision {
	mac := hmac.New(sha256.New, key)
	writeRevisionField(mac, []byte(revisionDomain))
	writeRevisionField(mac, []byte(path))
	if exists {
		writeRevisionField(mac, []byte("present"))
		writeRevisionField(mac, raw)
	} else {
		writeRevisionField(mac, []byte("missing"))
		writeRevisionField(mac, nil)
	}
	return Revision(revisionPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}

func writeRevisionField(mac hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = mac.Write(size[:])
	_, _ = mac.Write(value)
}

func canonicalPath(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", errors.New("configstore: path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve config path: %w", err)
	}
	abs = filepath.Clean(abs)
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("configstore: config path must not be a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect config path: %w", err)
	}
	parent, err := canonicalParent(filepath.Dir(abs))
	if err != nil {
		return "", "", fmt.Errorf("resolve config directory: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	identity := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	return abs, identity, nil
}

func canonicalParent(path string) (string, error) {
	current := path
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func loadOrCreateKey(path string) ([]byte, error) {
	key, err := readRevisionKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, revisionKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate revision key: %w", err)
	}
	stored, err := protectRevisionKey(key)
	if err != nil {
		return nil, fmt.Errorf("protect revision key: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, stored, revisionKeyMode); err != nil {
		return nil, fmt.Errorf("write revision key: %w", err)
	}
	return readRevisionKey(path)
}

func readRevisionKey(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open revision key: %w", err)
	}
	named, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if named.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(info, named) {
		return nil, fmt.Errorf("%w: sidecar is not a regular file", ErrRevisionKeyInvalid)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != revisionKeyMode {
		return nil, fmt.Errorf("%w: mode is %04o", ErrRevisionKeyInvalid, info.Mode().Perm())
	}
	stored, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read revision key: %w", err)
	}
	key, err := unprotectRevisionKey(stored)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRevisionKeyInvalid, err)
	}
	if len(key) != revisionKeySize {
		return nil, fmt.Errorf("%w: invalid plaintext size %d", ErrRevisionKeyInvalid, len(key))
	}
	return key, nil
}
