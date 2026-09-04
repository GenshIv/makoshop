package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestHandleHomeOffers verifies the home offers endpoint end-to-end on a
// temp store: sections for categories with products, cached vs fresh
// responses, and per-section item shape.
func TestHandleHomeOffers(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := db.NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	h := NewHandlers(store)

	if err := h.categoryRepo.Create(&model.Category{ID: 1, NameEn: "Electronics", NameRu: "Электроника", Slug: "electronics", IsActive: true}); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := h.categoryRepo.Create(&model.Category{ID: 2, NameEn: "Home", NameRu: "Дом", Slug: "home", IsActive: true}); err != nil {
		t.Fatalf("create category 2: %v", err)
	}
	mkPages := func(catID int64, n, idOffset int) {
		for i := 0; i < n; i++ {
			p := &model.EANPage{
				EAN:          fmt.Sprintf("590%08d", idOffset+i),
				Slug:         fmt.Sprintf("product-%d", idOffset+i),
				Title:        fmt.Sprintf("Product %d", idOffset+i),
				CategoryID:   catID,
				Currency:     "PLN",
				MinPrice:     float64(idOffset + i + 1),
				ProductCount: 1,
				IsActive:     true,
			}
			if err := h.eanPageRepo.Create(p); err != nil {
				t.Fatalf("create eanpage %d: %v", idOffset+i, err)
			}
		}
	}
	mkPages(1, 20, 0)
	mkPages(2, 8, 100)

	// Refresh the precomputed public tree (filters categories without EAN
	// pages via catalog sort-index counts — build the sort indexes FIRST)
	// and flush to disk.
	if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
		t.Fatalf("build sort indexes: %v", err)
	}
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store: %v", err)
	}
	h.categoryRepo.RebuildTrees()

	call := func(fresh bool) map[string]any {
		t.Helper()
		url := "/home/offers"
		if fresh {
			url += "?fresh=1"
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		h.HandleHomeOffers(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		return body
	}

	// Reset the shared package-level cache so the test is self-contained.
	homeOffersPayload = nil

	// Default mode (no settings): sections for every root category with
	// products plus the trailing "random products" pseudo-section.
	first := call(true)
	sections, ok := first["sections"].([]any)
	if !ok || len(sections) != 3 {
		t.Fatalf("expected 3 sections (2 categories + random), got %v", first["sections"])
	}
	pseudo := sections[2].(map[string]any)
	pseudoCat := pseudo["category"].(map[string]any)
	if pseudoCat["id"] != float64(0) || pseudoCat["url"] != "/shop" || pseudoCat["name_ru"] != "Случайные товары" {
		t.Fatalf("unexpected pseudo-section category: %v", pseudoCat)
	}
	if pseudo["total"] != float64(28) {
		t.Fatalf("expected pseudo-section total 28 (whole catalog), got %v", pseudo["total"])
	}
	if n := len(pseudo["items"].([]any)); n < 1 || n > 12 {
		t.Fatalf("pseudo-section items = %d, want 1..12", n)
	}

	sections, _ = first["sections"].([]any)
	section := sections[0].(map[string]any)
	category := section["category"].(map[string]any)
	if category["url"] == "/shop/electronics" {
		if category["id"] != float64(1) || category["name_ru"] != "Электроника" {
			t.Fatalf("unexpected section category: %v", category)
		}
		if section["total"] != float64(20) {
			t.Fatalf("expected section total 20, got %v", section["total"])
		}
		items := section["items"].([]any)
		if len(items) < 8 || len(items) > 12 {
			t.Fatalf("expected 8..12 items by default (tail windows may be short), got %d", len(items))
		}
		firstItem := items[0].(map[string]any)
		if firstItem["title"] == "" || firstItem["min_price"] == nil {
			t.Fatalf("item missing fields required by ProductCard: %v", firstItem)
		}
	}

	// Cache behavior, deterministic: without fresh=1 the stored payload is
	// returned as-is; fresh=1 rebuilds and must not return the stored one.
	sentinel := []byte(`{"sentinel":true}`)
	homeOffersPayload = sentinel

	req := httptest.NewRequest(http.MethodGet, "/home/offers", nil)
	rec := httptest.NewRecorder()
	h.HandleHomeOffers(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(sentinel) {
		t.Fatalf("cached call must return stored payload verbatim, got: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.HandleHomeOffers(rec, httptest.NewRequest(http.MethodGet, "/home/offers?fresh=1", nil))
	if rec.Code != http.StatusOK || rec.Body.String() == string(sentinel) {
		t.Fatalf("fresh call must rebuild the payload, got: %s", rec.Body.String())
	}

	// Method check.
	req = httptest.NewRequest(http.MethodPost, "/home/offers", nil)
	rec = httptest.NewRecorder()
	h.HandleHomeOffers(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}

	// Configured mode: ordered category list + carousel size override.
	settingsJSON := []byte(`{"home_offers": {"category_ids": [2, 1], "per_section": 5}}`)
	if err := store.DocPut("global_settings", settingsJSON); err != nil {
		t.Fatalf("put global_settings: %v", err)
	}
	configured := call(true)
	sections = configured["sections"].([]any)
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections in configured mode (2 categories + random), got %d", len(sections))
	}
	if got := sections[0].(map[string]any)["category"].(map[string]any)["id"]; got != float64(2) {
		t.Fatalf("configured order violated: first section category id = %v, want 2", got)
	}
	if got := sections[1].(map[string]any)["category"].(map[string]any)["id"]; got != float64(1) {
		t.Fatalf("configured order violated: second section category id = %v, want 1", got)
	}
	for _, s := range sections {
		items := s.(map[string]any)["items"].([]any)
		if len(items) > 5 {
			t.Fatalf("per_section override ignored: %d items", len(items))
		}
	}

	// Clearing the list returns to the default mode (all roots).
	settingsJSON = []byte(`{"home_offers": {"category_ids": [], "per_section": 0}}`)
	if err := store.DocPut("global_settings", settingsJSON); err != nil {
		t.Fatalf("put global_settings: %v", err)
	}
	defaults := call(true)
	if n := len(defaults["sections"].([]any)); n != 3 {
		t.Fatalf("expected 3 sections after clearing the list (2 categories + random), got %d", n)
	}
}

// TestNormalizeHomeOffers covers validation of the admin home offers payload.
func TestNormalizeHomeOffers(t *testing.T) {
	// nil payload → zero values (not configured).
	ids, per, err := normalizeHomeOffers(nil)
	if err != nil || ids != nil || per != 0 {
		t.Fatalf("nil payload: ids=%v per=%d err=%v", ids, per, err)
	}

	// Order preserved, duplicates dropped, non-numeric entries skipped.
	ids, per, err = normalizeHomeOffers(map[string]interface{}{
		"category_ids": []interface{}{float64(7), float64(3), float64(7)},
		"per_section":  float64(5),
	})
	if err != nil {
		t.Fatalf("valid payload: %v", err)
	}
	if len(ids) != 2 || ids[0] != 7 || ids[1] != 3 {
		t.Fatalf("ids = %v, want [7 3]", ids)
	}
	if per != 5 {
		t.Fatalf("per_section = %d, want 5", per)
	}

	// Invalid id.
	if _, _, err := normalizeHomeOffers(map[string]interface{}{
		"category_ids": []interface{}{float64(-1)},
	}); err == nil {
		t.Fatalf("expected error for negative id")
	}

	// per_section out of range.
	if _, _, err := normalizeHomeOffers(map[string]interface{}{
		"per_section": float64(21),
	}); err == nil {
		t.Fatalf("expected error for per_section > 20")
	}

	// Too many categories.
	tooMany := make([]interface{}, homeOffersMaxSectionsSetting+1)
	for i := range tooMany {
		tooMany[i] = float64(i + 1)
	}
	if _, _, err := normalizeHomeOffers(map[string]interface{}{
		"category_ids": tooMany,
	}); err == nil {
		t.Fatalf("expected error for too many categories")
	}
}
