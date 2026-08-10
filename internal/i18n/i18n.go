package i18n

import (
	"encoding/json"
	"sync"
)

var (
	mu       sync.RWMutex
	messages = map[string]map[string]string{} // lang -> key -> value
	current  = "en"
)

// Load loads translations from JSON data for the given language.
func Load(lang string, data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	mu.Lock()
	messages[lang] = m
	mu.Unlock()
	return nil
}

// SetCurrent sets the current language.
func SetCurrent(lang string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := messages[lang]; ok {
		current = lang
	}
}

// Current returns the current language.
func Current() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the translated string for the given key in the current language.
// Falls back to English, then to the key itself.
func T(key string) string {
	mu.RLock()
	defer mu.RUnlock()

	if m, ok := messages[current]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if m, ok := messages["en"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}

// TLang returns the translated string for the given key in the specified language.
func TLang(lang, key string) string {
	mu.RLock()
	defer mu.RUnlock()

	if m, ok := messages[lang]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if m, ok := messages["en"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}
