package db

import (
	"encoding/json"
	"fmt"
)

// CompanyPricesDoc stores all prices for a company's products in a single document.
// This enables fast price lookups and change detection during import without
// loading individual product documents.
type CompanyPricesDoc struct {
	Version int64             `json:"version"` // incremented on each write
	Prices  map[int64]float64 `json:"prices"`  // product ID -> price
}

const companyPricesKeyPrefix = "company_prices:"

// LoadCompanyPrices loads the price document for a company. Returns nil if not found.
func (r *ProductRepo) LoadCompanyPrices(companyID int64) (*CompanyPricesDoc, error) {
	key := companyPricesKeyPrefix + fmt.Sprintf("%d", companyID)
	data, err := r.store.DocGet(key)
	if err != nil || data == nil {
		return nil, nil // not found
	}
	var doc CompanyPricesDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal company prices for %d: %w", companyID, err)
	}
	return &doc, nil
}

// SaveCompanyPrices saves the price document for a company.
func (r *ProductRepo) SaveCompanyPrices(companyID int64, doc *CompanyPricesDoc) error {
	key := companyPricesKeyPrefix + fmt.Sprintf("%d", companyID)
	doc.Version++
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal company prices for %d: %w", companyID, err)
	}
	return r.store.DocPut(key, data)
}

// SaveCompanyPricesTx saves the price document within a transaction.
func (r *ProductRepo) SaveCompanyPricesTx(txn *Transaction, companyID int64, doc *CompanyPricesDoc) error {
	key := companyPricesKeyPrefix + fmt.Sprintf("%d", companyID)
	doc.Version++
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal company prices for %d: %w", companyID, err)
	}
	return txn.DocPut(key, data)
}

// InitCompanyPrices creates an empty price document for a company if it doesn't exist.
func (r *ProductRepo) InitCompanyPrices(companyID int64) (*CompanyPricesDoc, error) {
	doc, err := r.LoadCompanyPrices(companyID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		doc = &CompanyPricesDoc{
			Version: 0,
			Prices:  make(map[int64]float64),
		}
		if err := r.SaveCompanyPrices(companyID, doc); err != nil {
			return nil, fmt.Errorf("init company prices for %d: %w", companyID, err)
		}
	}
	return doc, nil
}
