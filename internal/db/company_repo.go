package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type CompanyRepo struct {
	Store *Store
}

func NewCompanyRepo(store *Store) *CompanyRepo {
	return &CompanyRepo{Store: store}
}

// Create creates a new company.
func (r *CompanyRepo) Create(c *model.Company) error {
	id, err := r.Store.NextID("company")
	if err != nil {
		return fmt.Errorf("next_id company: %w", err)
	}
	c.ID = id
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	if c.Status == "" {
		c.Status = model.CompanyStatusPending
	}
	if c.Settings.Currency == "" {
		c.Settings.Currency = "RUB"
	}

	data := MarshalCompany(*c)
	if err := r.Store.DocPut(KeyCompany(c.ID), data); err != nil {
		return fmt.Errorf("save company: %w", err)
	}

	// Index: company by owner user
	ownerKey := fmt.Sprintf("company:owner:%d", c.OwnerUserID)
	if err := r.Store.DocPut(ownerKey, []byte(fmt.Sprintf("%d", c.ID))); err != nil {
		_ = r.Store.DocDelete(KeyCompany(c.ID))
		return fmt.Errorf("index company owner: %w", err)
	}

	return nil
}

// Get returns a company by ID.
func (r *CompanyRepo) Get(id int64) (*model.Company, error) {
	data, err := r.Store.DocGet(KeyCompany(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("company %d not found", id)
		}
		return nil, fmt.Errorf("get company %d: %w", id, err)
	}
	return UnmarshalCompany(data)
}

// GetCompanyIDByUserID returns the company ID for a given user (seller).
func (r *CompanyRepo) GetCompanyIDByUserID(userID int64) (int64, error) {
	ownerKey := fmt.Sprintf("company:owner:%d", userID)
	data, err := r.Store.DocGet(ownerKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, fmt.Errorf("no company found for user %d", userID)
		}
		return 0, fmt.Errorf("get company by owner: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return id, nil
}

// Update updates a company.
func (r *CompanyRepo) Update(id int64, updater func(*model.Company)) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}

	updater(c)
	c.UpdatedAt = time.Now()

	data := MarshalCompany(*c)
	if err := r.Store.DocPut(KeyCompany(c.ID), data); err != nil {
		return fmt.Errorf("update company: %w", err)
	}

	return nil
}

// List returns all companies. Note: without a turbo index for companies,
// this is a placeholder. In production, maintain a company_list turbo index.
func (r *CompanyRepo) List() ([]model.Company, error) {
	return nil, nil
}

// Delete removes a company.
func (r *CompanyRepo) Delete(id int64) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}

	_ = r.Store.DocDelete(fmt.Sprintf("company:owner:%d", c.OwnerUserID))
	if err := r.Store.DocDelete(KeyCompany(id)); err != nil {
		return fmt.Errorf("delete company: %w", err)
	}
	return nil
}
