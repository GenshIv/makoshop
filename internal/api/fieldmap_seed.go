package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// allegroFieldMapSeed is the confirmed Allegro (laptopy-491) field map, built
// from real allegro.pl product pages. It is the default starting point for a
// company's editable field map: the UI loads it, the user reviews/edits it, and
// the result is stored per company in PriceSource.FieldMap.
//
//go:embed allegro_field_map.json
var allegroFieldMapSeed []byte

// seedFieldMapDoc is the on-disk shape of the seed file. Fields values may be
// either a plain name string ("attr_11323": "Stan") or a full FieldMapEntry
// object ("attr_11323": {"name": "Stan", "name_ru": "..."}); both are accepted.
type seedFieldMapDoc struct {
	Source   string                     `json:"source"`
	Category string                     `json:"category"`
	Note     string                     `json:"note"`
	Fields   map[string]json.RawMessage `json:"fields"`
	Special  map[string]string          `json:"special"`
	Skip     []string                   `json:"skip"`
}

// parseSeedFieldValue converts a seed field value (string name or object) into
// a FieldMapEntry.
func parseSeedFieldValue(raw json.RawMessage) (model.FieldMapEntry, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return model.FieldMapEntry{Name: s}, nil
	}
	var e model.FieldMapEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return model.FieldMapEntry{}, err
	}
	return e, nil
}

// DefaultFieldMap returns the confirmed Allegro field map as a code->entry map
// ready to be stored in a company's PriceSource.FieldMap. Special feed fields
// (gtin, EAN, category_id) are folded into the map as well; entries marked
// "skip" in the seed are stored with Skip=true so the importer ignores them.
func DefaultFieldMap() (map[string]model.FieldMapEntry, error) {
	var doc seedFieldMapDoc
	if err := json.Unmarshal(allegroFieldMapSeed, &doc); err != nil {
		return nil, fmt.Errorf("parse field-map seed: %w", err)
	}
	out := make(map[string]model.FieldMapEntry, len(doc.Fields)+len(doc.Special)+len(doc.Skip))
	for code, raw := range doc.Fields {
		entry, err := parseSeedFieldValue(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", code, err)
		}
		out[code] = entry
	}
	for code, name := range doc.Special {
		out[code] = model.FieldMapEntry{Name: name}
	}
	for _, code := range doc.Skip {
		out[code] = model.FieldMapEntry{Skip: true}
	}
	return out, nil
}

// HandleAdminFieldMapDefault returns the confirmed default field map so the UI
// can load it as a starting point for a company's editable field map.
// GET /admin/field-map/default
func (h *AuthHandlers) HandleAdminFieldMapDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	m, err := DefaultFieldMap()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"source": "allegro",
		"fields": m,
	})
}

// HandleAdminCompanyFieldMapSeed seeds a company's field map from the confirmed
// default (and sets its price-source format to "allegro" if unset). The user can
// then edit the map in the UI and re-save.
// POST /admin/companies/{id}/field-map/seed
func (h *AuthHandlers) HandleAdminCompanyFieldMapSeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	// Path: /admin/companies/{id}/field-map/seed
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "companies", "{id}", "field-map", "seed"]
	if len(parts) < 6 || parts[4] != "field-map" || parts[5] != "seed" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}
	m, err := DefaultFieldMap()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if err := h.companyRepo.Update(id, func(c *model.Company) {
		c.PriceSource.FieldMap = m
		if strings.TrimSpace(c.PriceSource.Format) == "" {
			c.PriceSource.Format = "allegro"
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status": "seeded",
		"fields": len(m),
	})
}
