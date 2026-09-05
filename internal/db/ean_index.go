package db

import (
	"encoding/json"
	"fmt"
)

// EAN index: one small document per page key (EAN or name-based key) holding
// the product IDs that share it. Together with the company price documents
// this lets the page recalculation compute ProductCount and MinPrice without
// reading any product documents and without touching makodb-internal tokens.
type EANIndexDoc struct {
	ProductIDs []int64 `json:"ids"`
}

const eanIndexKeyPrefix = "ean_products:"

func eanIndexKey(pageKey string) string {
	return eanIndexKeyPrefix + pageKey
}

// LoadEANIndexDoc loads the EAN index document for a page key. When txn is
// non-nil, buffered transaction writes take precedence over committed state
// (read-your-writes across import batches). Returns (nil, nil) when absent.
func LoadEANIndexDoc(txn *Transaction, store *Store, pageKey string) (*EANIndexDoc, error) {
	key := eanIndexKey(pageKey)
	var data []byte
	if txn != nil {
		if buf, ok := txn.DocGetBuffered(key); ok {
			data = buf
		}
	}
	if data == nil {
		val, err := store.DocGet(key)
		if err != nil || len(val) == 0 {
			return nil, nil // not found
		}
		data = val
	}
	var doc EANIndexDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal ean index %q: %w", pageKey, err)
	}
	return &doc, nil
}

// SaveEANIndexDoc writes the EAN index document — buffered in the transaction
// when one is active, directly otherwise.
func SaveEANIndexDoc(txn *Transaction, store *Store, pageKey string, doc *EANIndexDoc) error {
	if doc == nil {
		return nil
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal ean index %q: %w", pageKey, err)
	}
	if txn != nil {
		return txn.DocPut(eanIndexKey(pageKey), data)
	}
	return store.DocPut(eanIndexKey(pageKey), data)
}

// appendEANIndexIDs loads the document for pageKey, adds the given product IDs
// (deduplicated) and writes it back. Buffered loads make this safe across
// repeated calls within one transaction.
func appendEANIndexIDs(txn *Transaction, store *Store, pageKey string, ids []int64) error {
	if pageKey == "" || len(ids) == 0 {
		return nil
	}
	doc, err := LoadEANIndexDoc(txn, store, pageKey)
	if err != nil {
		return err
	}
	if doc == nil {
		doc = &EANIndexDoc{}
	}
	seen := make(map[int64]struct{}, len(doc.ProductIDs)+len(ids))
	for _, id := range doc.ProductIDs {
		seen[id] = struct{}{}
	}
	changed := false
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		doc.ProductIDs = append(doc.ProductIDs, id)
		changed = true
	}
	if !changed {
		return nil
	}
	return SaveEANIndexDoc(txn, store, pageKey, doc)
}

// removeEANIndexID loads the document for pageKey, drops the product ID and
// writes the document back. Missing documents are a no-op.
func removeEANIndexID(txn *Transaction, store *Store, pageKey string, id int64) error {
	if pageKey == "" || id == 0 {
		return nil
	}
	doc, err := LoadEANIndexDoc(txn, store, pageKey)
	if err != nil || doc == nil {
		return err
	}
	filtered := make([]int64, 0, len(doc.ProductIDs))
	removed := false
	for _, existing := range doc.ProductIDs {
		if existing == id {
			removed = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !removed {
		return nil
	}
	doc.ProductIDs = filtered
	return SaveEANIndexDoc(txn, store, pageKey, doc)
}
