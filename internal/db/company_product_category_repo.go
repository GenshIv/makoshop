package db

import (
	"encoding/json"
	"errors"
	"fmt"
)

// CompanyProductCategory maps a company's product to its assigned category.
// This is the reference table: company_id -> product_ean -> target_category_id.
type CompanyProductCategory struct {
	CompanyID        int64  `json:"company_id"`
	ProductEAN       string `json:"product_ean"`
	TargetCategoryID int64  `json:"target_category_id"`
}

// CompanyProductCategoryRepo manages the company->product->category reference table.
type CompanyProductCategoryRepo struct {
	store *Store
}

func NewCompanyProductCategoryRepo(store *Store) *CompanyProductCategoryRepo {
	return &CompanyProductCategoryRepo{store: store}
}

const (
	keyPrefixCPC = "cpc:" // company_product_category
)

// cpcKey builds the storage key for a company-product-category mapping.
func cpcKey(companyID int64, ean string) string {
	return fmt.Sprintf("%s%d:%s", keyPrefixCPC, companyID, ean)
}

// Upsert saves or updates a company-product-category mapping.
func (r *CompanyProductCategoryRepo) Upsert(m *CompanyProductCategory) error {
	if r.store == nil || m == nil {
		return fmt.Errorf("repo not initialized")
	}
	key := cpcKey(m.CompanyID, m.ProductEAN)
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}
	return r.store.DocPut(key, data)
}

// BatchUpsert saves multiple mappings in a single transaction.
func (r *CompanyProductCategoryRepo) BatchUpsertTx(txn *Transaction, mappings []*CompanyProductCategory) error {
	if txn == nil || len(mappings) == 0 {
		return nil
	}
	for _, m := range mappings {
		key := cpcKey(m.CompanyID, m.ProductEAN)
		data, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("serialize: %w", err)
		}
		if err := txn.DocPut(key, data); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
	}
	return nil
}

// Get retrieves a company-product-category mapping.
func (r *CompanyProductCategoryRepo) Get(companyID int64, ean string) (*CompanyProductCategory, error) {
	if r.store == nil {
		return nil, fmt.Errorf("repo not initialized")
	}
	key := cpcKey(companyID, ean)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	var m CompanyProductCategory
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return &m, nil
}

// Delete removes a company-product-category mapping.
func (r *CompanyProductCategoryRepo) Delete(companyID int64, ean string) error {
	if r.store == nil {
		return fmt.Errorf("repo not initialized")
	}
	key := cpcKey(companyID, ean)
	return r.store.DocDelete(key)
}
