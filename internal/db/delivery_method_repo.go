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
	turboKeyDeliveryMethods = "delivery_methods_list"
)

type DeliveryMethodRepo struct {
	store *Store
}

func NewDeliveryMethodRepo(store *Store) *DeliveryMethodRepo {
	return &DeliveryMethodRepo{store: store}
}

func keyDeliveryMethod(id int64) string {
	return fmt.Sprintf("doc:delivery_method:%d", id)
}

func keyDeliveryMethodBySlug(slug string) string {
	return fmt.Sprintf("doc:delivery_method:slug:%s", strings.ToLower(slug))
}

func (r *DeliveryMethodRepo) Create(dm *model.DeliveryMethod) error {
	if dm.Name == "" {
		return fmt.Errorf("name is required")
	}

	if dm.Slug == "" {
		dm.Slug = r.generateSlug(dm.Name)
	}

	slugKey := keyDeliveryMethodBySlug(dm.Slug)
	existing, err := r.store.DocGet(slugKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("check slug uniqueness: %w", err)
	}
	if existing != nil && len(existing) > 0 {
		return fmt.Errorf("slug already exists: %s", dm.Slug)
	}

	id, err := r.store.NextID("delivery_method")
	if err != nil {
		return fmt.Errorf("generate delivery method id: %w", err)
	}
	dm.ID = id
	dm.CreatedAt = time.Now().Unix()
	dm.UpdatedAt = time.Now().Unix()
	if dm.SortOrder == 0 {
		dm.SortOrder = 999
	}

	key := keyDeliveryMethod(id)
	data, err := json.Marshal(dm)
	if err != nil {
		return fmt.Errorf("marshal delivery method: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("store delivery method: %w", err)
	}

	slugData := []byte(fmt.Sprintf("%d", id))
	if err := r.store.DocPut(slugKey, slugData); err != nil {
		_ = r.store.DocDelete(key)
		return fmt.Errorf("store slug index: %w", err)
	}

	if err := r.indexDeliveryMethod(id); err != nil {
		_ = r.store.DocDelete(key)
		_ = r.store.DocDelete(slugKey)
		return fmt.Errorf("index delivery method: %w", err)
	}

	return nil
}

func (r *DeliveryMethodRepo) Get(id int64) (*model.DeliveryMethod, error) {
	key := keyDeliveryMethod(id)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("delivery method %d not found", id)
		}
		return nil, fmt.Errorf("get delivery method %d: %w", id, err)
	}
	var dm model.DeliveryMethod
	if err := json.Unmarshal(data, &dm); err != nil {
		return nil, fmt.Errorf("unmarshal delivery method %d: %w", id, err)
	}
	return &dm, nil
}

func (r *DeliveryMethodRepo) GetBySlug(slug string) (*model.DeliveryMethod, error) {
	slugKey := keyDeliveryMethodBySlug(slug)
	data, err := r.store.DocGet(slugKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("delivery method with slug %s not found", slug)
		}
		return nil, fmt.Errorf("get delivery method by slug: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

func (r *DeliveryMethodRepo) Update(id int64, updateFn func(*model.DeliveryMethod)) error {
	dm, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSlug := dm.Slug
	updateFn(dm)
	dm.UpdatedAt = time.Now().Unix()

	if dm.Slug != "" && strings.ToLower(dm.Slug) != strings.ToLower(oldSlug) {
		slugKey := keyDeliveryMethodBySlug(dm.Slug)
		existing, err := r.store.DocGet(slugKey)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return fmt.Errorf("check slug uniqueness: %w", err)
		}
		if existing != nil && len(existing) > 0 {
			var existingID int64
			_, _ = fmt.Sscanf(string(existing), "%d", &existingID)
			if existingID != id {
				return fmt.Errorf("slug already exists: %s", dm.Slug)
			}
		}
		if oldSlug != "" {
			_ = r.store.DocDelete(keyDeliveryMethodBySlug(oldSlug))
		}
		slugData := []byte(fmt.Sprintf("%d", id))
		if err := r.store.DocPut(slugKey, slugData); err != nil {
			return fmt.Errorf("store new slug index: %w", err)
		}
	}

	key := keyDeliveryMethod(id)
	data, err := json.Marshal(dm)
	if err != nil {
		return fmt.Errorf("marshal delivery method: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update delivery method: %w", err)
	}

	return nil
}

func (r *DeliveryMethodRepo) Delete(id int64) error {
	dm, err := r.Get(id)
	if err != nil {
		return err
	}

	key := keyDeliveryMethod(id)
	if err := r.store.DocDelete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("delete delivery method: %w", err)
	}

	if dm.Slug != "" {
		_ = r.store.DocDelete(keyDeliveryMethodBySlug(dm.Slug))
	}

	if err := r.unindexDeliveryMethod(id); err != nil {
		fmt.Printf("WARN: failed to unindex delivery method %d: %v\n", id, err)
	}

	return nil
}

func (r *DeliveryMethodRepo) List() ([]model.DeliveryMethod, error) {
	// Get delivery method IDs as Key128
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyDeliveryMethods)
	if err != nil || len(tokens) == 0 {
		return []model.DeliveryMethod{}, nil
	}
	// Use MultiGetByDocIDs to retrieve all delivery methods at once (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get delivery methods: %w", err)
	}
	var result []model.DeliveryMethod
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		var dm model.DeliveryMethod
		if err := json.Unmarshal(doc, &dm); err != nil {
			continue
		}
		result = append(result, dm)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].SortOrder < result[j].SortOrder
	})

	return result, nil
}

func (r *DeliveryMethodRepo) indexDeliveryMethod(id int64) error {
	_, err := r.store.db.TurboPutIndexString(turboKeyDeliveryMethods, keyDeliveryMethod(id))
	return err
}

func (r *DeliveryMethodRepo) unindexDeliveryMethod(id int64) error {
	_, err := r.store.db.TurboDeleteIndexString(turboKeyDeliveryMethods, keyDeliveryMethod(id))
	return err
}

func (r *DeliveryMethodRepo) generateSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = regexp.MustCompile(`[^a-z0-9а-яё-]`).ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "delivery-method"
	}
	return s
}
