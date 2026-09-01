package db

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

// SEORepo reads and writes the single configurable SEO / structured-data
// (JSON-LD) settings document.
type SEORepo struct {
	store *Store
}

func NewSEORepo(store *Store) *SEORepo {
	return &SEORepo{store: store}
}

// DefaultSettings returns a sane default configuration: JSON-LD enabled, with
// the standard search template. Organization/store fields are left empty so
// the admin can fill them in; the builders fall back to derived values
// (site name from base URL) when fields are empty.
func DefaultSettings() *model.SEOSettings {
	return &model.SEOSettings{
		Enabled:           true,
		SearchURLTemplate: model.SEODefaultSearch,
		PriceValidDays:    model.SEODefaultValid,
	}
}

// GetSettings returns the stored settings, or the defaults when the document
// does not exist yet. A stored document is always merged over the defaults so
// newly added fields keep working values.
func (r *SEORepo) GetSettings() (*model.SEOSettings, error) {
	data, err := r.store.DocGet(KeySEOSettings)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return DefaultSettings(), nil
		}
		return nil, err
	}
	s := DefaultSettings()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

// SaveSettings validates and persists the settings document.
func (r *SEORepo) SaveSettings(s *model.SEOSettings) error {
	if err := model.ValidateSEOSettings(s); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().Unix()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return r.store.DocPut(KeySEOSettings, b)
}
