package db

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyInstallmentPlans = "installment_plans_list"
)

type InstallmentPlanRepo struct {
	store *Store
}

func NewInstallmentPlanRepo(store *Store) *InstallmentPlanRepo {
	return &InstallmentPlanRepo{store: store}
}

func keyInstallmentPlan(id int64) string {
	return fmt.Sprintf("doc:installment_plan:%d", id)
}

func keyInstallmentPlanBySlug(slug string) string {
	return fmt.Sprintf("doc:installment_plan:slug:%s", strings.ToLower(slug))
}

func (r *InstallmentPlanRepo) Create(ip *model.InstallmentPlan) error {
	if ip.Name == "" {
		return fmt.Errorf("name is required")
	}

	if ip.Slug == "" {
		ip.Slug = r.generateSlug(ip.Name)
	}

	slugKey := keyInstallmentPlanBySlug(ip.Slug)
	existing, err := r.store.DocGet(slugKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("check slug uniqueness: %w", err)
	}
	if existing != nil && len(existing) > 0 {
		return fmt.Errorf("slug already exists: %s", ip.Slug)
	}

	id, err := r.store.NextID("installment_plan")
	if err != nil {
		return fmt.Errorf("generate installment plan id: %w", err)
	}
	ip.ID = id
	ip.CreatedAt = time.Now().Unix()
	ip.UpdatedAt = time.Now().Unix()
	if ip.SortOrder == 0 {
		ip.SortOrder = 999
	}

	key := keyInstallmentPlan(id)
	data, err := json.Marshal(ip)
	if err != nil {
		return fmt.Errorf("marshal installment plan: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("store installment plan: %w", err)
	}

	slugData := []byte(fmt.Sprintf("%d", id))
	if err := r.store.DocPut(slugKey, slugData); err != nil {
		_ = r.store.DocDelete(key)
		return fmt.Errorf("store slug index: %w", err)
	}

	if err := r.indexInstallmentPlan(id); err != nil {
		_ = r.store.DocDelete(key)
		_ = r.store.DocDelete(slugKey)
		return fmt.Errorf("index installment plan: %w", err)
	}

	return nil
}

func (r *InstallmentPlanRepo) Get(id int64) (*model.InstallmentPlan, error) {
	key := keyInstallmentPlan(id)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("installment plan %d not found", id)
		}
		return nil, fmt.Errorf("get installment plan %d: %w", id, err)
	}
	var ip model.InstallmentPlan
	if err := json.Unmarshal(data, &ip); err != nil {
		return nil, fmt.Errorf("unmarshal installment plan %d: %w", id, err)
	}
	return &ip, nil
}

func (r *InstallmentPlanRepo) GetBySlug(slug string) (*model.InstallmentPlan, error) {
	slugKey := keyInstallmentPlanBySlug(slug)
	data, err := r.store.DocGet(slugKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("installment plan with slug %s not found", slug)
		}
		return nil, fmt.Errorf("get installment plan by slug: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

func (r *InstallmentPlanRepo) Update(id int64, updateFn func(*model.InstallmentPlan)) error {
	ip, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSlug := ip.Slug
	updateFn(ip)
	ip.UpdatedAt = time.Now().Unix()

	if ip.Slug != "" && strings.ToLower(ip.Slug) != strings.ToLower(oldSlug) {
		slugKey := keyInstallmentPlanBySlug(ip.Slug)
		existing, err := r.store.DocGet(slugKey)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return fmt.Errorf("check slug uniqueness: %w", err)
		}
		if existing != nil && len(existing) > 0 {
			var existingID int64
			_, _ = fmt.Sscanf(string(existing), "%d", &existingID)
			if existingID != id {
				return fmt.Errorf("slug already exists: %s", ip.Slug)
			}
		}
		if oldSlug != "" {
			_ = r.store.DocDelete(keyInstallmentPlanBySlug(oldSlug))
		}
		slugData := []byte(fmt.Sprintf("%d", id))
		if err := r.store.DocPut(slugKey, slugData); err != nil {
			return fmt.Errorf("store new slug index: %w", err)
		}
	}

	key := keyInstallmentPlan(id)
	data, err := json.Marshal(ip)
	if err != nil {
		return fmt.Errorf("marshal installment plan: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update installment plan: %w", err)
	}

	return nil
}

func (r *InstallmentPlanRepo) Delete(id int64) error {
	ip, err := r.Get(id)
	if err != nil {
		return err
	}

	key := keyInstallmentPlan(id)
	if err := r.store.DocDelete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("delete installment plan: %w", err)
	}

	if ip.Slug != "" {
		_ = r.store.DocDelete(keyInstallmentPlanBySlug(ip.Slug))
	}

	if err := r.unindexInstallmentPlan(id); err != nil {
		fmt.Printf("WARN: failed to unindex installment plan %d: %v\n", id, err)
	}

	return nil
}

func (r *InstallmentPlanRepo) List() ([]model.InstallmentPlan, error) {
	// Get installment plan IDs as Key128
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyInstallmentPlans)
	if err != nil || len(tokens) == 0 {
		return []model.InstallmentPlan{}, nil
	}
	// Use MultiGetByDocIDs to retrieve all installment plans at once (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get installment plans: %w", err)
	}
	var result []model.InstallmentPlan
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		var ip model.InstallmentPlan
		if err := json.Unmarshal(doc, &ip); err != nil {
			continue
		}
		result = append(result, ip)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].SortOrder < result[j].SortOrder
	})

	return result, nil
}

func (r *InstallmentPlanRepo) indexInstallmentPlan(id int64) error {
	_, err := r.store.db.TurboPutIndexString(turboKeyInstallmentPlans, strconv.Itoa(int(id)))
	return err
}

func (r *InstallmentPlanRepo) unindexInstallmentPlan(id int64) error {
	_, err := r.store.db.TurboDeleteIndexString(turboKeyInstallmentPlans, strconv.Itoa(int(id)))
	return err
}

func (r *InstallmentPlanRepo) generateSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = regexp.MustCompile(`[^a-z0-9а-яё-]`).ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "installment-plan"
	}
	return s
}
