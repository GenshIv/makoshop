package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

type PromoCampaignRepo struct {
	store *Store
}

func NewPromoCampaignRepo(store *Store) *PromoCampaignRepo {
	return &PromoCampaignRepo{store: store}
}

func (r *PromoCampaignRepo) Create(c *model.PromoCampaign) error {
	id, err := r.store.NextID("promo_campaign")
	if err != nil {
		return fmt.Errorf("next_id promo_campaign: %w", err)
	}
	c.ID = id
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	if c.Status == "" {
		c.Status = model.PromoCampaignStatusPending
	}

	data := MarshalPromoCampaign(*c)
	if err := r.store.DocPut(KeyPromoCampaign(c.ID), data); err != nil {
		return fmt.Errorf("save promo_campaign: %w", err)
	}

	// Index by company (turbo)
	companyKey := fmt.Sprintf("promo_campaign_company:%d", c.CompanyID)
	if _, err := r.store.db.TurboPutIndex(companyKey, uint64(c.ID)); err != nil {
		_ = r.store.DocDelete(KeyPromoCampaign(c.ID))
		return fmt.Errorf("turbo index campaign by company: %w", err)
	}

	// Index in global list (turbo)
	if _, err := r.store.db.TurboPutIndex(turboKeyAllPromoCampaigns, uint64(c.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndex(companyKey, uint64(c.ID))
		_ = r.store.DocDelete(KeyPromoCampaign(c.ID))
		return fmt.Errorf("turbo index campaign global: %w", err)
	}

	return nil
}

func (r *PromoCampaignRepo) Get(id int64) (*model.PromoCampaign, error) {
	data, err := r.store.DocGet(KeyPromoCampaign(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("promo_campaign %d not found", id)
		}
		return nil, fmt.Errorf("get promo_campaign %d: %w", id, err)
	}
	return UnmarshalPromoCampaign(data)
}

func (r *PromoCampaignRepo) ListByCompany(companyID int64) ([]model.PromoCampaign, error) {
	key := fmt.Sprintf("promo_campaign_company:%d", companyID)
	data, err := r.store.db.TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.PromoCampaign
	for _, id := range ids {
		c, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *c)
	}

	// Sort by created_at desc
	for i := len(result)/2 - 1; i >= 0; i-- {
		j := len(result) - 1 - i
		if result[i].CreatedAt.Before(result[j].CreatedAt) {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, nil
}

func (r *PromoCampaignRepo) Update(id int64, updater func(*model.PromoCampaign)) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(c)
	c.UpdatedAt = time.Now()
	data := MarshalPromoCampaign(*c)
	return r.store.DocPut(KeyPromoCampaign(c.ID), data)
}

func (r *PromoCampaignRepo) UpdateStatus(id int64, status model.PromoCampaignStatus) error {
	return r.Update(id, func(c *model.PromoCampaign) {
		c.Status = status
	})
}

func (r *PromoCampaignRepo) Delete(id int64) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}
	companyKey := fmt.Sprintf("promo_campaign_company:%d", c.CompanyID)
	_, _ = r.store.db.TurboDeleteIndex(companyKey, uint64(id))
	_, _ = r.store.db.TurboDeleteIndex(turboKeyAllPromoCampaigns, uint64(id))
	return r.store.DocDelete(KeyPromoCampaign(id))
}

// turboKeyAllPromoCampaigns is the turbo index key for all promo campaigns.
const turboKeyAllPromoCampaigns = "promo_campaign_list"

// GetAllCampaigns returns all campaigns (for analytics/admin). Uses turbo index.
func (r *PromoCampaignRepo) GetAllCampaigns() ([]model.PromoCampaign, error) {
	data, err := r.store.db.TurboRawRead(turboKeyAllPromoCampaigns)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.PromoCampaign
	for _, id := range ids {
		c, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *c)
	}

	return result, nil
}

// ListAll returns all campaigns (alias for GetAllCampaigns).
func (r *PromoCampaignRepo) ListAll() ([]model.PromoCampaign, error) {
	return r.GetAllCampaigns()
}

// GetActiveCampaigns returns all active campaigns. Uses turbo index.
func (r *PromoCampaignRepo) GetActiveCampaigns() ([]model.PromoCampaign, error) {
	data, err := r.store.db.TurboRawRead(turboKeyAllPromoCampaigns)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	now := time.Now()
	var result []model.PromoCampaign

	for _, id := range ids {
		c, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		if c.Status == model.PromoCampaignStatusActive && !c.EndAt.Before(now) {
			result = append(result, *c)
		}
	}

	return result, nil
}
