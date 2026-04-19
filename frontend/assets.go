package frontend

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

//go:embed dist/*
var assets embed.FS

//go:embed src/i18n/locales/*.json
var localeAssets embed.FS

var (
	localeCache   = map[string]map[string]any{}
	localeCacheMu sync.RWMutex
)

func DistFS() (fs.FS, error) {
	return fs.Sub(assets, "dist")
}

func LocaleValue(language string, path ...string) (string, bool, error) {
	locale, err := loadLocale(language)
	if err != nil {
		return "", false, err
	}
	var current any = locale
	for _, key := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return "", false, nil
		}
		current, ok = next[key]
		if !ok {
			return "", false, nil
		}
	}
	value, ok := current.(string)
	return value, ok, nil
}

func loadLocale(language string) (map[string]any, error) {
	localeCacheMu.RLock()
	if locale, ok := localeCache[language]; ok {
		localeCacheMu.RUnlock()
		return locale, nil
	}
	localeCacheMu.RUnlock()

	data, err := localeAssets.ReadFile(fmt.Sprintf("src/i18n/locales/%s.json", language))
	if err != nil {
		return nil, err
	}
	var locale map[string]any
	if err := json.Unmarshal(data, &locale); err != nil {
		return nil, err
	}
	localeCacheMu.Lock()
	localeCache[language] = locale
	localeCacheMu.Unlock()
	return locale, nil
}
