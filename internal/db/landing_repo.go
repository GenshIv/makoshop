package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyLandingList = "landing_list"
	turboKeyLandingSCU  = "landing_scu:" // prefix for SCU lookup
)

type LandingRepo struct {
	Store *Store
}

func NewLandingRepo(store *Store) *LandingRepo {
	return &LandingRepo{Store: store}
}

// Create creates a new landing page.
func (r *LandingRepo) Create(l *model.LandingPage) error {
	if l.SCU == "" {
		return fmt.Errorf("scu is required")
	}

	id, err := r.Store.NextID("landing")
	if err != nil {
		return fmt.Errorf("next_id landing: %w", err)
	}
	l.ID = id
	l.CreatedAt = time.Now().Unix()
	l.UpdatedAt = time.Now().Unix()
	if l.Slug == "" {
		l.Slug = toLandingSlug(l.SCU)
	}

	data := MarshalLandingPage(*l)
	if err := r.Store.DocPut(KeyLandingPage(l.ID), data); err != nil {
		return fmt.Errorf("save landing: %w", err)
	}

	// Turbo index: landing_list
	if _, err := r.Store.db.TurboPutIndex(turboKeyLandingList, uint64(id)); err != nil {
		_ = r.Store.DocDelete(KeyLandingPage(l.ID))
		return fmt.Errorf("turbo index landing_list: %w", err)
	}

	// Turbo index: landing_scu:<scu>
	scuKey := turboKeyLandingSCU + l.SCU
	if err := r.Store.TurboWrite(scuKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndex(turboKeyLandingList, uint64(id))
		_ = r.Store.DocDelete(KeyLandingPage(l.ID))
		return fmt.Errorf("turbo index landing_scu: %w", err)
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

// GetBySCU returns a landing page by SCU.
func (r *LandingRepo) GetBySCU(scu string) (*model.LandingPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is empty")
	}
	scuKey := turboKeyLandingSCU + scu
	data, err := r.Store.db.TurboRawRead(scuKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("landing page with scu %q not found", scu)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetBySlug returns a landing page by slug.
func (r *LandingRepo) GetBySlug(slug string) (*model.LandingPage, error) {
	// Slugs are derived from SCU, so we can lookup via SCU
	scu := slugToSCU(slug)
	return r.GetBySCU(scu)
}

// Update updates a landing page.
func (r *LandingRepo) Update(id int64, updater func(*model.LandingPage)) error {
	l, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSCU := l.SCU
	updater(l)
	l.UpdatedAt = time.Now().Unix()

	data := MarshalLandingPage(*l)
	if err := r.Store.DocPut(KeyLandingPage(l.ID), data); err != nil {
		return fmt.Errorf("update landing: %w", err)
	}

	// Update SCU index if changed
	if oldSCU != l.SCU {
		_ = r.Store.TurboWrite(turboKeyLandingSCU+oldSCU, []byte{}) // clear old
		if l.SCU != "" {
			if err := r.Store.TurboWrite(turboKeyLandingSCU+l.SCU, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update landing_scu index: %w", err)
			}
		}
	}

	return nil
}

// List returns all landing pages via turbo index.
func (r *LandingRepo) List() ([]model.LandingPage, error) {
	data, err := r.Store.db.TurboRawRead(turboKeyLandingList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.LandingPage
	for _, id := range ids {
		l, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *l)
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
	_, _ = r.Store.db.TurboDeleteIndex(turboKeyLandingList, uint64(id))
	if l.SCU != "" {
		_ = r.Store.TurboWrite(turboKeyLandingSCU+l.SCU, []byte{})
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

// UpsertBySCU creates or updates a landing page by SCU.
// If a landing page with this SCU exists, updates it.
// If not, creates a new one.
func (r *LandingRepo) UpsertBySCU(scu string, updater func(*model.LandingPage)) (*model.LandingPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is required")
	}

	// Try to get existing
	l, err := r.GetBySCU(scu)
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
		SCU:      scu,
		Slug:     toLandingSlug(scu),
		Title:    scu,
		IsActive: true,
	}
	updater(l)
	if err := r.Create(l); err != nil {
		return nil, err
	}
	return l, nil
}

// --- helpers ---

// toLandingSlug creates a URL-friendly slug from SCU.
func toLandingSlug(scu string) string {
	s := strings.ToLower(scu)
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

// slugToSCU reverses the slug back to SCU (approximate).
func slugToSCU(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}
