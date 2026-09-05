package db

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// EAN page ID registry: small documents holding just the page IDs, sharded by
// ID range (bucket = id/registryBucket). Full-catalog maintenance passes read
// page IDs from these light docs instead of loading every page document just
// to learn its ID. Maintained on page create/delete; a backfill from the
// index-paginated path self-heals databases created before the registry.

const registryBucket = 10000

const eanpageIDsKeyPrefix = "eanpage_ids:"

// EANPageIDsDoc is one registry bucket: page IDs in [b*10000, (b+1)*10000).
type EANPageIDsDoc struct {
	IDs []int64 `json:"ids"`
}

func eanpageIDsKey(id int64) string {
	return eanpageIDsKeyPrefix + strconv.FormatInt(id/registryBucket, 10)
}

// loadIDsDoc reads one registry bucket (read-your-writes inside a transaction).
func loadIDsDoc(txn *Transaction, store *Store, key string) (*EANPageIDsDoc, error) {
	var data []byte
	if txn != nil {
		if buf, ok := txn.DocGetBuffered(key); ok {
			data = buf
		}
	}
	if data == nil {
		val, err := store.DocGet(key)
		if err != nil || len(val) == 0 {
			return nil, nil
		}
		data = val
	}
	var doc EANPageIDsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return &doc, nil
}

func saveIDsDoc(txn *Transaction, store *Store, key string, doc *EANPageIDsDoc) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if txn != nil {
		return txn.DocPut(key, data)
	}
	return store.DocPut(key, data)
}

// RegisterEANPageID adds a page ID to its registry bucket (idempotent).
func RegisterEANPageID(txn *Transaction, store *Store, id int64) error {
	if id <= 0 {
		return nil
	}
	key := eanpageIDsKey(id)
	doc, err := loadIDsDoc(txn, store, key)
	if err != nil {
		return err
	}
	if doc == nil {
		doc = &EANPageIDsDoc{}
	}
	for _, existing := range doc.IDs {
		if existing == id {
			return nil
		}
	}
	doc.IDs = append(doc.IDs, id)
	return saveIDsDoc(txn, store, key, doc)
}

// UnregisterEANPageID removes a page ID from its registry bucket.
func UnregisterEANPageID(txn *Transaction, store *Store, id int64) error {
	if id <= 0 {
		return nil
	}
	key := eanpageIDsKey(id)
	doc, err := loadIDsDoc(txn, store, key)
	if err != nil || doc == nil {
		return err
	}
	filtered := doc.IDs[:0]
	for _, existing := range doc.IDs {
		if existing != id {
			filtered = append(filtered, existing)
		}
	}
	if len(filtered) == len(doc.IDs) {
		return nil
	}
	doc.IDs = filtered
	return saveIDsDoc(txn, store, key, doc)
}

// LoadEANPageIDsFromRegistry returns all registered page IDs by walking the
// bucket range derived from the ID counter. Missing buckets are simply empty.
func LoadEANPageIDsFromRegistry(store *Store) []int64 {
	data, err := store.DocGet("state:next_id:eanpage")
	if err != nil || len(data) == 0 {
		return nil
	}
	var maxID int64
	if _, err := fmt.Sscanf(string(data), "%d", &maxID); err != nil || maxID <= 0 {
		return nil
	}
	ids := make([]int64, 0, maxID)
	for b := int64(0); b <= maxID/registryBucket; b++ {
		doc, err := loadIDsDoc(nil, store, eanpageIDsKeyPrefix+strconv.FormatInt(b, 10))
		if err != nil || doc == nil {
			continue
		}
		ids = append(ids, doc.IDs...)
	}
	return ids
}

// SaveEANPageIDsToRegistry persists the ID set into bucket documents
// (backfill for databases whose pages predate the registry).
func SaveEANPageIDsToRegistry(store *Store, ids []int64) error {
	buckets := make(map[int64][]int64)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		b := id / registryBucket
		buckets[b] = append(buckets[b], id)
	}
	for b, list := range buckets {
		key := eanpageIDsKeyPrefix + strconv.FormatInt(b, 10)
		if err := saveIDsDoc(nil, store, key, &EANPageIDsDoc{IDs: list}); err != nil {
			return err
		}
	}
	return nil
}
