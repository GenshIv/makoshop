package db

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"encoding/json"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyDeliveryTimes = "delivery_times_list"
)

type DeliveryTimeRepo struct {
	store *Store
}

func NewDeliveryTimeRepo(store *Store) *DeliveryTimeRepo {
	return &DeliveryTimeRepo{store: store}
}

func keyDeliveryTime(id int64) string {
	return fmt.Sprintf("doc:delivery_time:%d", id)
}

func keyDeliveryTimeBySlug(slug string) string {
	return fmt.Sprintf("doc:delivery_time:slug:%s", strings.ToLower(slug))
}

func (r *DeliveryTimeRepo) Create(dt *model.DeliveryTime) error {
	if dt.Name == "" {
		return fmt.Errorf("name is required")
	}

	if dt.Slug == "" {
		dt.Slug = r.generateSlug(dt.Name)
	}

	slugKey := keyDeliveryTimeBySlug(dt.Slug)
	existing, err := r.store.DocGet(slugKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("check slug uniqueness: %w", err)
	}
	if existing != nil && len(existing) > 0 {
		return fmt.Errorf("slug already exists: %s", dt.Slug)
	}

	id, err := r.store.NextID("delivery_time")
	if err != nil {
		return fmt.Errorf("generate delivery time id: %w", err)
	}
	dt.ID = id
	dt.CreatedAt = time.Now()
	dt.UpdatedAt = time.Now()
	if dt.SortOrder == 0 {
		dt.SortOrder = 999
	}

	key := keyDeliveryTime(id)
	data, err := json.Marshal(dt)
	if err != nil {
		return fmt.Errorf("marshal delivery time: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("store delivery time: %w", err)
	}

	slugData := []byte(fmt.Sprintf("%d", id))
	if err := r.store.DocPut(slugKey, slugData); err != nil {
		_ = r.store.DocDelete(key)
		return fmt.Errorf("store slug index: %w", err)
	}

	if err := r.indexDeliveryTime(id); err != nil {
		_ = r.store.DocDelete(key)
		_ = r.store.DocDelete(slugKey)
		return fmt.Errorf("index delivery time: %w", err)
	}

	return nil
}

func (r *DeliveryTimeRepo) Get(id int64) (*model.DeliveryTime, error) {
	key := keyDeliveryTime(id)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("delivery time %d not found", id)
		}
		return nil, fmt.Errorf("get delivery time %d: %w", id, err)
	}
	var dt model.DeliveryTime
	if err := json.Unmarshal(data, &dt); err != nil {
		return nil, fmt.Errorf("unmarshal delivery time %d: %w", id, err)
	}
	return &dt, nil
}

func (r *DeliveryTimeRepo) GetBySlug(slug string) (*model.DeliveryTime, error) {
	slugKey := keyDeliveryTimeBySlug(slug)
	data, err := r.store.DocGet(slugKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("delivery time with slug %s not found", slug)
		}
		return nil, fmt.Errorf("get delivery time by slug: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

func (r *DeliveryTimeRepo) Update(id int64, updateFn func(*model.DeliveryTime)) error {
	dt, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSlug := dt.Slug
	updateFn(dt)
	dt.UpdatedAt = time.Now()

	if dt.Slug != "" && strings.ToLower(dt.Slug) != strings.ToLower(oldSlug) {
		slugKey := keyDeliveryTimeBySlug(dt.Slug)
		existing, err := r.store.DocGet(slugKey)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return fmt.Errorf("check slug uniqueness: %w", err)
		}
		if existing != nil && len(existing) > 0 {
			var existingID int64
			_, _ = fmt.Sscanf(string(existing), "%d", &existingID)
			if existingID != id {
				return fmt.Errorf("slug already exists: %s", dt.Slug)
			}
		}
		if oldSlug != "" {
			_ = r.store.DocDelete(keyDeliveryTimeBySlug(oldSlug))
		}
		slugData := []byte(fmt.Sprintf("%d", id))
		if err := r.store.DocPut(slugKey, slugData); err != nil {
			return fmt.Errorf("store new slug index: %w", err)
		}
	}

	key := keyDeliveryTime(id)
	data, err := json.Marshal(dt)
	if err != nil {
		return fmt.Errorf("marshal delivery time: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update delivery time: %w", err)
	}

	return nil
}

func (r *DeliveryTimeRepo) Delete(id int64) error {
	dt, err := r.Get(id)
	if err != nil {
		return err
	}

	key := keyDeliveryTime(id)
	if err := r.store.DocDelete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("delete delivery time: %w", err)
	}

	if dt.Slug != "" {
		_ = r.store.DocDelete(keyDeliveryTimeBySlug(dt.Slug))
	}

	if err := r.unindexDeliveryTime(id); err != nil {
		fmt.Printf("WARN: failed to unindex delivery time %d: %v\n", id, err)
	}

	return nil
}

func (r *DeliveryTimeRepo) List() ([]model.DeliveryTime, error) {
	data, err := r.store.db.TurboRawRead(turboKeyDeliveryTimes)
	if err != nil || len(data) == 0 {
		return []model.DeliveryTime{}, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.DeliveryTime
	for _, id := range ids {
		dt, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *dt)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].SortOrder < result[j].SortOrder
	})

	return result, nil
}

func (r *DeliveryTimeRepo) indexDeliveryTime(id int64) error {
	_, err := r.store.db.TurboPutIndex(turboKeyDeliveryTimes, uint64(id))
	return err
}

func (r *DeliveryTimeRepo) unindexDeliveryTime(id int64) error {
	_, err := r.store.db.TurboDeleteIndex(turboKeyDeliveryTimes, uint64(id))
	return err
}

func (r *DeliveryTimeRepo) generateSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = regexp.MustCompile(`[^a-z0-9а-яё-]`).ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "delivery-time"
	}
	return s
}
