package db

import (
	"encoding/json"
	"fmt"
	"time"
)

// CompanyPricesDoc stores all prices for a company's products in a single document.
// This enables fast price lookups and change detection during import without
// loading individual product documents.
type CompanyPricesDoc struct {
	Version int64             `json:"version"` // incremented on each write
	Prices  map[int64]float64 `json:"prices"`  // product ID -> price
}

const companyPricesKeyPrefix = "company_prices:"
const companyPriceVersionsKeyPrefix = "company_price_versions:"

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

// LoadCompanyPriceVersions loads the version history (array of unix timestamps) for a company.
func (r *ProductRepo) LoadCompanyPriceVersions(companyID int64) ([]int64, error) {
	key := companyPriceVersionsKeyPrefix + fmt.Sprintf("%d", companyID)
	data, err := r.store.DocGet(key)
	if err != nil || data == nil {
		return []int64{}, nil // not found
	}
	var versions []int64
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, fmt.Errorf("unmarshal price versions for %d: %w", companyID, err)
	}
	return versions, nil
}

// SaveCompanyPriceVersions saves the version history for a company.
func (r *ProductRepo) SaveCompanyPriceVersions(companyID int64, versions []int64) error {
	key := companyPriceVersionsKeyPrefix + fmt.Sprintf("%d", companyID)
	data, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal price versions for %d: %w", companyID, err)
	}
	return r.store.DocPut(key, data)
}

// SaveCompanyPriceVersionsTx saves the version history within a transaction.
func (r *ProductRepo) SaveCompanyPriceVersionsTx(txn *Transaction, companyID int64, versions []int64) error {
	key := companyPriceVersionsKeyPrefix + fmt.Sprintf("%d", companyID)
	data, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal price versions for %d: %w", companyID, err)
	}
	return txn.DocPut(key, data)
}

// LoadCompanyPricesAt loads the price document from a specific version (unix timestamp).
func (r *ProductRepo) LoadCompanyPricesAt(companyID int64, timestamp int64) (*CompanyPricesDoc, error) {
	key := fmt.Sprintf("%s%d:%d", companyPricesKeyPrefix, companyID, timestamp)
	data, err := r.store.DocGet(key)
	if err != nil || data == nil {
		return nil, nil // not found
	}
	var doc CompanyPricesDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal company prices at %d for %d: %w", timestamp, companyID, err)
	}
	return &doc, nil
}

// savePriceVersion saves a versioned copy of the price document with current timestamp.
func (r *ProductRepo) savePriceVersion(companyID int64, doc *CompanyPricesDoc) error {
	timestamp := time.Now().Unix()
	key := fmt.Sprintf("%s%d:%d", companyPricesKeyPrefix, companyID, timestamp)
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal price version for %d at %d: %w", companyID, timestamp, err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("save price version for %d at %d: %w", companyID, timestamp, err)
	}

	// Update versions index
	versions, err := r.LoadCompanyPriceVersions(companyID)
	if err != nil {
		return fmt.Errorf("load price versions for %d: %w", companyID, err)
	}
	versions = append(versions, timestamp)
	return r.SaveCompanyPriceVersions(companyID, versions)
}

// savePriceVersionTx saves a versioned copy within a transaction.
func (r *ProductRepo) savePriceVersionTx(txn *Transaction, companyID int64, doc *CompanyPricesDoc) error {
	timestamp := time.Now().Unix()
	key := fmt.Sprintf("%s%d:%d", companyPricesKeyPrefix, companyID, timestamp)
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal price version for %d at %d: %w", companyID, timestamp, err)
	}
	if err := txn.DocPut(key, data); err != nil {
		return fmt.Errorf("save price version for %d at %d: %w", companyID, timestamp, err)
	}

	// Update versions index - read from store directly
	versionsKey := companyPriceVersionsKeyPrefix + fmt.Sprintf("%d", companyID)
	versionsData, err := r.store.DocGet(versionsKey)
	if err != nil {
		return fmt.Errorf("load price versions for %d: %w", companyID, err)
	}
	var versions []int64
	if versionsData != nil {
		if err := json.Unmarshal(versionsData, &versions); err != nil {
			return fmt.Errorf("unmarshal price versions for %d: %w", companyID, err)
		}
	}
	versions = append(versions, timestamp)
	return r.SaveCompanyPriceVersionsTx(txn, companyID, versions)
}

// DeleteCompanyPrices deletes all price data for a company (current doc + versions).
func (r *ProductRepo) DeleteCompanyPrices(companyID int64) error {
	// Load version history to delete all versioned documents
	versions, err := r.LoadCompanyPriceVersions(companyID)
	if err != nil {
		return fmt.Errorf("load price versions for %d: %w", companyID, err)
	}

	// Delete each versioned document
	for _, timestamp := range versions {
		key := fmt.Sprintf("%s%d:%d", companyPricesKeyPrefix, companyID, timestamp)
		if err := r.store.DocDelete(key); err != nil {
			return fmt.Errorf("delete price version for %d at %d: %w", companyID, timestamp, err)
		}
	}

	// Delete current document
	currentKey := companyPricesKeyPrefix + fmt.Sprintf("%d", companyID)
	if err := r.store.DocDelete(currentKey); err != nil {
		return fmt.Errorf("delete current prices for %d: %w", companyID, err)
	}

	// Delete versions index
	versionsKey := companyPriceVersionsKeyPrefix + fmt.Sprintf("%d", companyID)
	if err := r.store.DocDelete(versionsKey); err != nil {
		return fmt.Errorf("delete price versions index for %d: %w", companyID, err)
	}

	return nil
}

// SaveCompanyPrices saves the price document for a company with versioning.
func (r *ProductRepo) SaveCompanyPrices(companyID int64, doc *CompanyPricesDoc) error {
	// Save versioned copy before updating current document
	if err := r.savePriceVersion(companyID, doc); err != nil {
		return fmt.Errorf("save price version for %d: %w", companyID, err)
	}

	key := companyPricesKeyPrefix + fmt.Sprintf("%d", companyID)
	doc.Version++
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal company prices for %d: %w", companyID, err)
	}
	return r.store.DocPut(key, data)
}

// SaveCompanyPricesTx saves the price document within a transaction with versioning.
func (r *ProductRepo) SaveCompanyPricesTx(txn *Transaction, companyID int64, doc *CompanyPricesDoc) error {
	// Save versioned copy before updating current document
	if err := r.savePriceVersionTx(txn, companyID, doc); err != nil {
		return fmt.Errorf("save price version for %d: %w", companyID, err)
	}

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
