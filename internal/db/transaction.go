package db

import (
	"fmt"
	"sync"

	"github.com/GenshIv/makoshop/internal/model"
)

// Transaction represents an atomic batch of database operations.
// All writes are buffered in memory and applied atomically on Commit.
type Transaction struct {
	store *Store

	mu sync.Mutex

	// Buffered document writes: key -> value
	docPuts map[string][]byte

	// Buffered turbo writes: key -> value
	turboWrites map[string][]byte

	// Buffered turbo batch index writes: token -> []docID
	turboBatchIndex map[string][]string

	// Buffered turbo sort index writes: token -> []docID (sorted)
	turboSortIndex map[string][]string

	// Buffered document deletions: key -> struct{}
	docDeletes map[string]struct{}

	// Buffered turbo index deletions: token -> set of docIDs
	turboIndexDeletes map[string]map[string]struct{}

	// Products created/updated in this transaction (for EAN page operations)
	products []*model.Product

	// Flag to indicate if transaction is active
	active bool

	// Flag to indicate if transaction has been committed or aborted
	finished bool
}

// NewTransaction creates a new transaction.
func NewTransaction(store *Store) *Transaction {
	return &Transaction{
		store:             store,
		docPuts:           make(map[string][]byte),
		turboWrites:       make(map[string][]byte),
		turboBatchIndex:   make(map[string][]string),
		turboSortIndex:    make(map[string][]string),
		docDeletes:        make(map[string]struct{}),
		turboIndexDeletes: make(map[string]map[string]struct{}),
		active:            true,
	}
}

// Begin starts the transaction.
func (t *Transaction) Begin() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.finished {
		return fmt.Errorf("transaction already finished")
	}

	t.active = true
	return nil
}

// Commit applies all buffered writes to the database atomically.
func (t *Transaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return fmt.Errorf("transaction not active")
	}
	if t.finished {
		return fmt.Errorf("transaction already finished")
	}

	// Apply all document puts
	for key, value := range t.docPuts {
		if err := t.store.DocPut(key, value); err != nil {
			return fmt.Errorf("commit doc put %s: %w", key, err)
		}
	}

	// Apply all turbo writes
	for key, value := range t.turboWrites {
		if err := t.store.TurboWrite(key, value); err != nil {
			return fmt.Errorf("commit turbo write %s: %w", key, err)
		}
	}

	// Apply all turbo batch index writes
	for token, docIDs := range t.turboBatchIndex {
		if len(docIDs) > 0 {
			if _, err := t.store.db.TurboPutBatchIndexString(token, docIDs); err != nil {
				return fmt.Errorf("commit turbo batch index %s: %w", token, err)
			}
		}
	}

	// Apply all turbo sort index writes
	for token, docIDs := range t.turboSortIndex {
		if len(docIDs) > 0 {
			if err := t.store.db.TurboPutSortIndexString(token, docIDs); err != nil {
				return fmt.Errorf("commit turbo sort index %s: %w", token, err)
			}
		}
	}

	// Apply all document deletions (after puts, so deletes take precedence)
	for key := range t.docDeletes {
		if err := t.store.DocDelete(key); err != nil {
			return fmt.Errorf("commit doc delete %s: %w", key, err)
		}
	}

	// Apply all turbo index deletions (after puts, so deletes take precedence)
	// Use batch deletion to minimize vacuum
	for token, docIDSet := range t.turboIndexDeletes {
		if len(docIDSet) > 0 {
			docIDs := make([]string, 0, len(docIDSet))
			for docID := range docIDSet {
				docIDs = append(docIDs, docID)
			}
			if _, err := t.store.db.TurboDeleteBatchIndexString(token, docIDs); err != nil {
				return fmt.Errorf("commit turbo batch delete index %s: %w", token, err)
			}
		}
	}

	t.active = false
	t.finished = true

	return nil
}

// Abort discards all buffered writes.
func (t *Transaction) Abort() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return fmt.Errorf("transaction not active")
	}
	if t.finished {
		return fmt.Errorf("transaction already finished")
	}

	// Clear all buffered writes
	t.docPuts = make(map[string][]byte)
	t.turboWrites = make(map[string][]byte)
	t.turboBatchIndex = make(map[string][]string)
	t.turboSortIndex = make(map[string][]string)
	t.docDeletes = make(map[string]struct{})
	t.turboIndexDeletes = make(map[string]map[string]struct{})
	t.products = nil

	t.active = false
	t.finished = true

	return nil
}

// IsActive returns true if the transaction is active.
func (t *Transaction) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active && !t.finished
}

// IsFinished returns true if the transaction has been committed or aborted.
func (t *Transaction) IsFinished() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finished
}

// DocPut buffers a document write.
func (t *Transaction) DocPut(key string, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return fmt.Errorf("transaction not active")
	}

	t.docPuts[key] = value
	return nil
}

// DocGetBuffered returns the buffered value for a document key written earlier
// in this transaction (read-your-writes). Returns ("", false) when the key was
// not written in this transaction; committed state must be read separately.
func (t *Transaction) DocGetBuffered(key string) ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.finished {
		return nil, false
	}
	if _, deleted := t.docDeletes[key]; deleted {
		return nil, true
	}
	val, ok := t.docPuts[key]
	return val, ok
}

// TurboWrite buffers a turbo index write.
func (t *Transaction) TurboWrite(key string, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return fmt.Errorf("transaction not active")
	}

	t.turboWrites[key] = value
	return nil
}

// TurboPutBatchIndexString buffers a turbo batch index write.
func (t *Transaction) TurboPutBatchIndexString(token string, docIDs []string) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return 0, fmt.Errorf("transaction not active")
	}

	t.turboBatchIndex[token] = append(t.turboBatchIndex[token], docIDs...)
	return len(docIDs), nil
}

// TurboPutSortIndexString buffers a turbo sort index write.
func (t *Transaction) TurboPutSortIndexString(token string, docIDs []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return fmt.Errorf("transaction not active")
	}

	t.turboSortIndex[token] = docIDs
	return nil
}

// TurboPutIndexString buffers a turbo index write (single docID).
func (t *Transaction) TurboPutIndexString(token string, docID string) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return 0, fmt.Errorf("transaction not active")
	}

	t.turboBatchIndex[token] = append(t.turboBatchIndex[token], docID)
	return 1, nil
}

// DocDelete buffers a document deletion.
func (t *Transaction) DocDelete(key string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return fmt.Errorf("transaction not active")
	}

	t.docDeletes[key] = struct{}{}
	return nil
}

// TurboDeleteIndexString buffers a turbo index deletion (remove docID from token).
func (t *Transaction) TurboDeleteIndexString(token string, docID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active || t.finished {
		return fmt.Errorf("transaction not active")
	}

	if t.turboIndexDeletes[token] == nil {
		t.turboIndexDeletes[token] = make(map[string]struct{})
	}
	t.turboIndexDeletes[token][docID] = struct{}{}
	return nil
}

// AddProduct adds a product to the transaction.
func (t *Transaction) AddProduct(p *model.Product) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.products = append(t.products, p)
}

// GetProducts returns all products in the transaction.
func (t *Transaction) GetProducts() []*model.Product {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make([]*model.Product, len(t.products))
	copy(result, t.products)
	return result
}

// Clear removes all buffered operations.
func (t *Transaction) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.docPuts = make(map[string][]byte)
	t.turboWrites = make(map[string][]byte)
	t.turboBatchIndex = make(map[string][]string)
	t.turboSortIndex = make(map[string][]string)
	t.docDeletes = make(map[string]struct{})
	t.turboIndexDeletes = make(map[string]map[string]struct{})
	t.products = nil
}

// Len returns the number of buffered operations.
func (t *Transaction) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.docPuts) + len(t.turboWrites) + len(t.turboBatchIndex)
}
