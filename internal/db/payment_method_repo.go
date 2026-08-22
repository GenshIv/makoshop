package db

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"encoding/json"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyPaymentMethods = "payment_methods_list"
)

type PaymentMethodRepo struct {
	store *Store
}

func NewPaymentMethodRepo(store *Store) *PaymentMethodRepo {
	return &PaymentMethodRepo{store: store}
}

func keyPaymentMethod(id int64) string {
	return fmt.Sprintf("doc:payment_method:%d", id)
}

func keyPaymentMethodBySlug(slug string) string {
	return fmt.Sprintf("doc:payment_method:slug:%s", strings.ToLower(slug))
}

func (r *PaymentMethodRepo) Create(pm *model.CompanyPaymentMethod) error {
	if pm.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Generate slug from name if empty
	if pm.Slug == "" {
		pm.Slug = r.generateSlug(pm.Name)
	}

	// Check slug uniqueness
	slugKey := keyPaymentMethodBySlug(pm.Slug)
	existing, err := r.store.DocGet(slugKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("check slug uniqueness: %w", err)
	}
	if existing != nil && len(existing) > 0 {
		return fmt.Errorf("slug already exists: %s", pm.Slug)
	}

	// Generate ID
	id, err := r.store.NextID("payment_method")
	if err != nil {
		return fmt.Errorf("generate payment method id: %w", err)
	}
	pm.ID = id
	pm.CreatedAt = time.Now().Unix()
	pm.UpdatedAt = time.Now().Unix()
	if pm.SortOrder == 0 {
		pm.SortOrder = 999
	}

	// Store document
	key := keyPaymentMethod(id)
	data, err := json.Marshal(pm)
	if err != nil {
		return fmt.Errorf("marshal payment method: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("store payment method: %w", err)
	}

	// Store slug index
	slugData := []byte(fmt.Sprintf("%d", id))
	if err := r.store.DocPut(slugKey, slugData); err != nil {
		_ = r.store.DocDelete(key)
		return fmt.Errorf("store slug index: %w", err)
	}

	// Index in turbo list
	if err := r.indexPaymentMethod(id); err != nil {
		_ = r.store.DocDelete(key)
		_ = r.store.DocDelete(slugKey)
		return fmt.Errorf("index payment method: %w", err)
	}

	return nil
}

func (r *PaymentMethodRepo) Get(id int64) (*model.CompanyPaymentMethod, error) {
	key := keyPaymentMethod(id)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("payment method %d not found", id)
		}
		return nil, fmt.Errorf("get payment method %d: %w", id, err)
	}
	var pm model.CompanyPaymentMethod
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, fmt.Errorf("unmarshal payment method %d: %w", id, err)
	}
	return &pm, nil
}

func (r *PaymentMethodRepo) GetBySlug(slug string) (*model.CompanyPaymentMethod, error) {
	slugKey := keyPaymentMethodBySlug(slug)
	data, err := r.store.DocGet(slugKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("payment method with slug %s not found", slug)
		}
		return nil, fmt.Errorf("get payment method by slug: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

func (r *PaymentMethodRepo) Update(id int64, updateFn func(*model.CompanyPaymentMethod)) error {
	pm, err := r.Get(id)
	if err != nil {
		return err
	}

	// Check slug uniqueness if changed
	oldSlug := pm.Slug
	updateFn(pm)
	pm.UpdatedAt = time.Now().Unix()

	if pm.Slug != "" && strings.ToLower(pm.Slug) != strings.ToLower(oldSlug) {
		slugKey := keyPaymentMethodBySlug(pm.Slug)
		existing, err := r.store.DocGet(slugKey)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return fmt.Errorf("check slug uniqueness: %w", err)
		}
		if existing != nil && len(existing) > 0 {
			var existingID int64
			_, _ = fmt.Sscanf(string(existing), "%d", &existingID)
			if existingID != id {
				return fmt.Errorf("slug already exists: %s", pm.Slug)
			}
		}
		// Remove old slug index
		if oldSlug != "" {
			_ = r.store.DocDelete(keyPaymentMethodBySlug(oldSlug))
		}
		// Store new slug index
		slugData := []byte(fmt.Sprintf("%d", id))
		if err := r.store.DocPut(slugKey, slugData); err != nil {
			return fmt.Errorf("store new slug index: %w", err)
		}
	}

	key := keyPaymentMethod(id)
	data, err := json.Marshal(pm)
	if err != nil {
		return fmt.Errorf("marshal payment method: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update payment method: %w", err)
	}

	return nil
}

func (r *PaymentMethodRepo) Delete(id int64) error {
	pm, err := r.Get(id)
	if err != nil {
		return err
	}

	// Delete document
	key := keyPaymentMethod(id)
	if err := r.store.DocDelete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("delete payment method: %w", err)
	}

	// Delete slug index
	if pm.Slug != "" {
		_ = r.store.DocDelete(keyPaymentMethodBySlug(pm.Slug))
	}

	// Remove from turbo list
	if err := r.unindexPaymentMethod(id); err != nil {
		// Non-fatal, but log
		fmt.Printf("WARN: failed to unindex payment method %d: %v\n", id, err)
	}

	return nil
}

func (r *PaymentMethodRepo) List() ([]model.CompanyPaymentMethod, error) {
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeyPaymentMethods)
	if err != nil || len(tokens) == 0 {
		return []model.CompanyPaymentMethod{}, nil
	}

	docs, err := r.store.db.MultiGetByDocIDsWithPrefix(tokens, "doc:payment_method:")
	if err != nil {
		return nil, fmt.Errorf("multi get payment methods: %w", err)
	}

	var result []model.CompanyPaymentMethod
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		var pm model.CompanyPaymentMethod
		if err := json.Unmarshal(doc, &pm); err != nil {
			continue
		}
		result = append(result, pm)
	}

	// Sort by SortOrder
	sort.Slice(result, func(i, j int) bool {
		return result[i].SortOrder < result[j].SortOrder
	})

	return result, nil
}

func (r *PaymentMethodRepo) indexPaymentMethod(id int64) error {
	_, err := r.store.db.TurboPutIndex(turboKeyPaymentMethods, KeyPaymentMethodKey128(id))
	return err
}

func (r *PaymentMethodRepo) unindexPaymentMethod(id int64) error {
	_, err := r.store.db.TurboDeleteIndex(turboKeyPaymentMethods, KeyPaymentMethodKey128(id))
	return err
}

func (r *PaymentMethodRepo) generateSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = regexp.MustCompile(`[^a-z0-9а-яё-]`).ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "payment-method"
	}
	return s
}
