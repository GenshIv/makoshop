package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyLandingList = "landing_list"
	turboKeyLandingSCU  = "landing_ean:" // prefix for EAN lookup
)

type LandingRepo struct {
	Store *Store
}

func NewLandingRepo(store *Store) *LandingRepo {
	return &LandingRepo{Store: store}
}

// Create creates a new landing page.
func (r *LandingRepo) Create(l *model.LandingPage) error {
	if l.EAN == "" {
		return fmt.Errorf("ean is required")
	}

	id, err := r.Store.NextID("landing")
	if err != nil {
		return fmt.Errorf("next_id landing: %w", err)
	}
	l.ID = id
	l.CreatedAt = time.Now().Unix()
	l.UpdatedAt = time.Now().Unix()
	if l.Slug == "" {
		l.Slug = toLandingSlug(l.EAN)
	}

	data := MarshalLandingPage(*l)
	if err := r.Store.DocPut(KeyLandingPage(l.ID), data); err != nil {
		return fmt.Errorf("save landing: %w", err)
	}

	// Turbo index: landing_list
	if _, err := r.Store.db.TurboPutIndexString(turboKeyLandingList, KeyLandingPage(l.ID)); err != nil {
		_ = r.Store.DocDelete(KeyLandingPage(l.ID))
		return fmt.Errorf("turbo index landing_list: %w", err)
	}

	// Turbo index: landing_ean:<ean>
	eanKey := turboKeyLandingSCU + l.EAN
	if err := r.Store.TurboWrite(eanKey, []byte(KeyLandingPage(l.ID))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndexString(turboKeyLandingList, KeyLandingPage(l.ID))
		_ = r.Store.DocDelete(KeyLandingPage(l.ID))
		return fmt.Errorf("turbo index landing_ean: %w", err)
	}

	return nil
}

// Get returns a landing page by ID.
func (r *LandingRepo) Get(id int64) (*model.LandingPage, error) {
	data, err := r.Store.DocGet(KeyLandingPage(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("landing page %d not found", id)
		}
		return nil, fmt.Errorf("get landing %d: %w", id, err)
	}
	return UnmarshalLandingPage(data)
}

// GetBySCU returns a landing page by EAN.
func (r *LandingRepo) GetByEAN(ean string) (*model.LandingPage, error) {
	if ean == "" {
		return nil, fmt.Errorf("ean is empty")
	}
	eanKey := turboKeyLandingSCU + ean
	data, err := r.Store.db.TurboRawRead(eanKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("landing page with ean %q not found", ean)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetBySlug returns a landing page by slug.
func (r *LandingRepo) GetBySlug(slug string) (*model.LandingPage, error) {
	// Slugs are derived from EAN, so we can lookup via EAN
	ean := slugToSCU(slug)
	return r.GetByEAN(ean)
}

// Update updates a landing page.
func (r *LandingRepo) Update(id int64, updater func(*model.LandingPage)) error {
	l, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSCU := l.EAN
	updater(l)
	l.UpdatedAt = time.Now().Unix()

	data := MarshalLandingPage(*l)
	if err := r.Store.DocPut(KeyLandingPage(l.ID), data); err != nil {
		return fmt.Errorf("update landing: %w", err)
	}

	// Update EAN index if changed
	if oldSCU != l.EAN {
		_ = r.Store.TurboWrite(turboKeyLandingSCU+oldSCU, []byte{}) // clear old
		if l.EAN != "" {
			if err := r.Store.TurboWrite(turboKeyLandingSCU+l.EAN, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update landing_scu index: %w", err)
			}
		}
	}

	return nil
}

// List returns all landing pages via turbo index.
func (r *LandingRepo) List() ([]model.LandingPage, error) {
	tokens, err := r.Store.db.TurboGetIndexTokens(turboKeyLandingList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	docs, err := r.Store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get landing docs: %w", err)
	}

	var result []model.LandingPage
	for _, doc := range docs {
		if len(doc) > 0 {
			l, err := UnmarshalLandingPage(doc)
			if err != nil {
				continue
			}
			result = append(result, *l)
		}
	}
	return result, nil
}

// Delete removes a landing page.
func (r *LandingRepo) Delete(id int64) error {
	l, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove turbo indexes
	_, _ = r.Store.db.TurboDeleteIndexString(turboKeyLandingList, KeyLandingPage(id))
	if l.EAN != "" {
		_ = r.Store.TurboWrite(turboKeyLandingSCU+l.EAN, []byte{})
	}

	if err := r.Store.DocDelete(KeyLandingPage(id)); err != nil {
		return fmt.Errorf("delete landing: %w", err)
	}
	return nil
}

// AddProduct adds a product ID to the landing page's product list.
func (r *LandingRepo) AddProduct(id int64, productID int64) error {
	l, err := r.Get(id)
	if err != nil {
		return err
	}

	// Check if already present
	for _, pid := range l.ProductIDs {
		if pid == productID {
			return nil // already there
		}
	}

	l.ProductIDs = append(l.ProductIDs, productID)
	l.UpdatedAt = time.Now().Unix()

	data := MarshalLandingPage(*l)
	return r.Store.DocPut(KeyLandingPage(l.ID), data)
}

// RemoveProduct removes a product ID from the landing page's product list.
func (r *LandingRepo) RemoveProduct(id int64, productID int64) error {
	l, err := r.Get(id)
	if err != nil {
		return err
	}

	var newIDs []int64
	for _, pid := range l.ProductIDs {
		if pid != productID {
			newIDs = append(newIDs, pid)
		}
	}

	l.ProductIDs = newIDs
	l.UpdatedAt = time.Now().Unix()

	data := MarshalLandingPage(*l)
	return r.Store.DocPut(KeyLandingPage(l.ID), data)
}

// UpsertBySCU creates or updates a landing page by EAN.
// If a landing page with this EAN exists, updates it.
// If not, creates a new one.
func (r *LandingRepo) UpsertByEAN(ean string, updater func(*model.LandingPage)) (*model.LandingPage, error) {
	if ean == "" {
		return nil, fmt.Errorf("ean is required")
	}

	// Try to get existing
	l, err := r.GetByEAN(ean)
	if err == nil {
		// Update existing
		updater(l)
		l.UpdatedAt = time.Now().Unix()
		data := MarshalLandingPage(*l)
		if err := r.Store.DocPut(KeyLandingPage(l.ID), data); err != nil {
			return nil, fmt.Errorf("update landing: %w", err)
		}
		return l, nil
	}

	// Create new
	l = &model.LandingPage{
		EAN:      ean,
		Slug:     toLandingSlug(ean),
		Title:    ean,
		IsActive: true,
	}
	updater(l)
	if err := r.Create(l); err != nil {
		return nil, err
	}
	return l, nil
}

// --- helpers ---

// toLandingSlug creates a URL-friendly slug from EAN.
func toLandingSlug(ean string) string {
	s := strings.ToLower(ean)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens
	result := []rune{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result = append(result, r)
		}
	}
	// Collapse multiple hyphens
	collapsed := []rune{}
	for i, r := range result {
		if r == '-' && i > 0 && result[i-1] == '-' {
			continue
		}
		collapsed = append(collapsed, r)
	}
	// Trim leading/trailing hyphens
	start, end := 0, len(collapsed)
	for start < end && collapsed[start] == '-' {
		start++
	}
	for end > start && collapsed[end-1] == '-' {
		end--
	}
	return string(collapsed[start:end])
}

// slugToSCU reverses the slug back to EAN (approximate).
func slugToSCU(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}
