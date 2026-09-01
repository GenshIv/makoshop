package api

import (
	"strings"
	"testing"
)

func TestNormalizeHomeHero(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		raw := map[string]interface{}{
			"ru": map[string]interface{}{
				"headline": "  Привет  ",
				"sub":      "под",
				"tagline":  "слоган",
			},
			"en": map[string]interface{}{
				"headline": "Hi",
			},
		}
		got, err := normalizeHomeHero(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["ru"]["headline"] != "Привет" {
			t.Errorf("ru.headline = %q, want trimmed", got["ru"]["headline"])
		}
		if got["ru"]["tagline"] != "слоган" {
			t.Errorf("ru.tagline = %q", got["ru"]["tagline"])
		}
		// Missing fields are normalized to empty strings (i18n fallback).
		if got["en"]["sub"] != "" {
			t.Errorf("en.sub = %q, want empty", got["en"]["sub"])
		}
		// Locales absent from the payload are not created.
		if _, ok := got["pl"]; ok {
			t.Errorf("pl should not be present")
		}
	})

	t.Run("unknown locales and fields are dropped", func(t *testing.T) {
		raw := map[string]interface{}{
			"xx": map[string]interface{}{"headline": "nope"},
			"ru": map[string]interface{}{"headline": "ok", "bogus": "field"},
		}
		got, err := normalizeHomeHero(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got["xx"]; ok {
			t.Errorf("unknown locale xx should be dropped")
		}
		if _, ok := got["ru"]["bogus"]; ok {
			t.Errorf("unknown field should be dropped")
		}
	})

	t.Run("wrong types are ignored", func(t *testing.T) {
		raw := map[string]interface{}{
			"ru": "not a map",
			"en": map[string]interface{}{"headline": 42},
		}
		got, err := normalizeHomeHero(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got["ru"]; ok {
			t.Errorf("ru with wrong type should be ignored")
		}
		if got["en"]["headline"] != "" {
			t.Errorf("en.headline = %q, want empty (non-string ignored)", got["en"]["headline"])
		}
	})

	t.Run("field too long", func(t *testing.T) {
		raw := map[string]interface{}{
			"ru": map[string]interface{}{"headline": strings.Repeat("a", 301)},
		}
		if _, err := normalizeHomeHero(raw); err == nil {
			t.Fatalf("expected error for field > %d chars", homeHeroFieldMaxLen)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		got, err := normalizeHomeHero(map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})
}
