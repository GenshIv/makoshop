package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyBrandSetList      = "branding_set_list"
	turboKeyBrandCatThemeList = "branding_cat_theme_list"
)

// BrandingRepo stores branding sets (named decoration packages) and
// per-category slot overrides. See docs/BRANDING_SYSTEM_PLAN.md.
type BrandingRepo struct {
	store *Store
}

func NewBrandingRepo(store *Store) *BrandingRepo {
	return &BrandingRepo{store: store}
}

// --- Brand sets ---

// CreateSet validates and stores a new branding set.
func (r *BrandingRepo) CreateSet(s *model.BrandSet) error {
	if err := model.ValidateBrandSet(s); err != nil {
		return err
	}
	id, err := r.store.NextID("branding_set")
	if err != nil {
		return fmt.Errorf("next_id branding_set: %w", err)
	}
	s.ID = id
	s.CreatedAt = time.Now().Unix()
	s.UpdatedAt = s.CreatedAt

	if err := r.store.DocPut(KeyBrandSet(id), MarshalBrandSet(*s)); err != nil {
		return fmt.Errorf("save brand set: %w", err)
	}
	if _, err := r.store.db.TurboPutIndexString(turboKeyBrandSetList, KeyBrandSet(id)); err != nil {
		_ = r.store.DocDelete(KeyBrandSet(id))
		return fmt.Errorf("turbo index branding_set_list: %w", err)
	}
	return nil
}

// GetSet returns a set by ID.
func (r *BrandingRepo) GetSet(id int64) (*model.BrandSet, error) {
	data, err := r.store.DocGet(KeyBrandSet(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("brand set %d not found", id)
		}
		return nil, fmt.Errorf("get brand set %d: %w", id, err)
	}
	return UnmarshalBrandSet(data)
}

// ListSets returns all sets (enabled and disabled), ordered by ID.
func (r *BrandingRepo) ListSets() ([]model.BrandSet, error) {
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeyBrandSetList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get brand sets: %w", err)
	}
	sets := make([]model.BrandSet, 0, len(docs))
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		s, err := UnmarshalBrandSet(doc)
		if err != nil {
			continue
		}
		sets = append(sets, *s)
	}
	// Stable order: by ID.
	for i := 1; i < len(sets); i++ {
		for j := i; j > 0 && sets[j].ID < sets[j-1].ID; j-- {
			sets[j], sets[j-1] = sets[j-1], sets[j]
		}
	}
	return sets, nil
}

// UpdateSet applies the updater to the set, re-validates and persists it.
func (r *BrandingRepo) UpdateSet(id int64, updater func(*model.BrandSet)) error {
	s, err := r.GetSet(id)
	if err != nil {
		return err
	}
	updater(s)
	if err := model.ValidateBrandSet(s); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().Unix()
	if err := r.store.DocPut(KeyBrandSet(id), MarshalBrandSet(*s)); err != nil {
		return fmt.Errorf("update brand set: %w", err)
	}
	return nil
}

// DeleteSet removes a set and its list index entry.
func (r *BrandingRepo) DeleteSet(id int64) error {
	if err := r.store.DocDelete(KeyBrandSet(id)); err != nil {
		return fmt.Errorf("delete brand set: %w", err)
	}
	_, _ = r.store.db.TurboDeleteIndexString(turboKeyBrandSetList, KeyBrandSet(id))
	return nil
}

// --- Category overrides ---

// UpsertCatTheme creates or updates the override for (categoryID, slot).
func (r *BrandingRepo) UpsertCatTheme(t *model.BrandCategoryTheme) error {
	if err := model.ValidateBrandCatTheme(t); err != nil {
		return err
	}
	key := KeyBrandCatTheme(t.CategoryID, t.Slot)

	existing, err := r.store.DocGet(key)
	if err == nil {
		// Update: keep the existing ID and CreatedAt.
		old, uerr := UnmarshalBrandCatTheme(existing)
		if uerr == nil {
			t.ID = old.ID
		}
		t.UpdatedAt = time.Now().Unix()
	} else if !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("get existing brand cat theme: %w", err)
	}
	if t.ID == 0 {
		id, err := r.store.NextID("branding_cat_theme")
		if err != nil {
			return fmt.Errorf("next_id branding_cat_theme: %w", err)
		}
		t.ID = id
		if t.CreatedAt == 0 {
			t.CreatedAt = time.Now().Unix()
		}
	}

	if err := r.store.DocPut(key, MarshalBrandCatTheme(*t)); err != nil {
		return fmt.Errorf("save brand cat theme: %w", err)
	}
	if _, err := r.store.db.TurboPutIndexString(turboKeyBrandCatThemeList, key); err != nil {
		return fmt.Errorf("turbo index branding_cat_theme_list: %w", err)
	}
	return nil
}

// GetCatTheme returns the override for (categoryID, slot) or a not-found error.
func (r *BrandingRepo) GetCatTheme(categoryID int64, slot model.BrandSlot) (*model.BrandCategoryTheme, error) {
	data, err := r.store.DocGet(KeyBrandCatTheme(categoryID, slot))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("brand cat theme %d/%s not found", categoryID, slot)
		}
		return nil, fmt.Errorf("get brand cat theme: %w", err)
	}
	return UnmarshalBrandCatTheme(data)
}

// ListCatThemes returns all category overrides, ordered by ID.
func (r *BrandingRepo) ListCatThemes() ([]model.BrandCategoryTheme, error) {
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeyBrandCatThemeList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get brand cat themes: %w", err)
	}
	themes := make([]model.BrandCategoryTheme, 0, len(docs))
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		t, err := UnmarshalBrandCatTheme(doc)
		if err != nil {
			continue
		}
		themes = append(themes, *t)
	}
	for i := 1; i < len(themes); i++ {
		for j := i; j > 0 && themes[j].ID < themes[j-1].ID; j-- {
			themes[j], themes[j-1] = themes[j-1], themes[j]
		}
	}
	return themes, nil
}

// DeleteCatTheme removes the override for (categoryID, slot).
func (r *BrandingRepo) DeleteCatTheme(categoryID int64, slot model.BrandSlot) error {
	key := KeyBrandCatTheme(categoryID, slot)
	if err := r.store.DocDelete(key); err != nil {
		return fmt.Errorf("delete brand cat theme: %w", err)
	}
	_, _ = r.store.db.TurboDeleteIndexString(turboKeyBrandCatThemeList, key)
	return nil
}

// --- Versioning ---

// GetVersion returns the current branding data version (0 if never written).
func (r *BrandingRepo) GetVersion() int64 {
	data, err := r.store.db.TurboRawRead(KeyBrandingVersion)
	if err != nil || len(data) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(string(data), 10, 64)
	return v
}

// BumpVersion increments the branding data version. Called after every
// admin write so clients can detect stale caches.
func (r *BrandingRepo) BumpVersion() error {
	v := r.GetVersion() + 1
	if err := r.store.db.TurboRawWrite(KeyBrandingVersion, []byte(strconv.FormatInt(v, 10))); err != nil {
		return fmt.Errorf("bump branding version: %w", err)
	}
	return nil
}
