package db

import (
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/pkg/config"
)

var ErrKeyNotFound = errors.New("key not found")

// ErrInvalidAttrKey is returned when a raw attribute key cannot be mapped to
// a valid attribute code (empty, a value, a sentence, starts with a digit…).
var ErrInvalidAttrKey = errors.New("invalid attribute key")

type Store struct {
	db *makodb.ShardedDB

	// Simple atomic ID generators per entity type (in-memory, persisted via turbo)
	nextIDs map[string]*atomic.Int64
}

func NewStore(cfg config.DatabaseConfig) (*Store, error) {
	s := &Store{
		nextIDs: make(map[string]*atomic.Int64),
	}

	var err error
	s.db, err = makodb.OpenSharded(
		cfg.Path,
		cfg.NumShards,
		cfg.MaxTotalSize,
		cfg.NumBucketsPerShard,
		true,
	)
	if err != nil {
		return nil, fmt.Errorf("open makodb: %w", err)
	}

	// Initialize ID generators from stored state or start from 1
	entityTypes := []string{
		"user", "company", "category", "attrdef", "product",
		"cart", "order", "payment", "review",
		"promo_plan", "promo_campaign", "promo_log",
	}
	for _, et := range entityTypes {
		key := fmt.Sprintf("state:next_id:%s", et)
		val, err := s.db.TurboRawRead(key)
		if err != nil || len(val) == 0 {
			s.nextIDs[et] = new(atomic.Int64)
			s.nextIDs[et].Store(1)
		} else {
			var id int64
			_, _ = fmt.Sscanf(string(val), "%d", &id)
			if id < 1 {
				id = 1
			}
			ai := new(atomic.Int64)
			ai.Store(id)
			s.nextIDs[et] = ai
		}
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying ShardedDB (for turbo index access, reads and writes).
func (s *Store) DB() *makodb.ShardedDB {
	return s.db
}

// TurboWrite writes a turbo key-value directly.
func (s *Store) TurboWrite(key string, value []byte) error {
	return s.db.TurboRawWrite(key, value)
}

// TurboWrite writes a turbo key-value directly.
func (s *Store) TurboDelete(key string) error {
	return s.db.TurboRawDelete(key)
}

// NextID generates and persists the next ID for the given entity type.
// On first call for an entity type, it reads the last persisted ID from DB.
func (s *Store) NextID(entityType string) (int64, error) {
	ai, ok := s.nextIDs[entityType]
	if !ok {
		// Read last ID from DB
		key := fmt.Sprintf("state:next_id:%s", entityType)
		data, _ := s.db.TurboRawRead(key)
		var lastID int64 = 0
		if len(data) > 0 {
			_, _ = fmt.Sscanf(string(data), "%d", &lastID)
		}

		ai = new(atomic.Int64)
		ai.Store(lastID)
		s.nextIDs[entityType] = ai
	}

	id := ai.Add(1)
	key := fmt.Sprintf("state:next_id:%s", entityType)
	if err := s.db.TurboRawWrite(key, []byte(fmt.Sprintf("%d", id))); err != nil {
		return id, fmt.Errorf("persist next_id %s: %w", entityType, err)
	}
	return id, nil
}

// SetNextIDIfGreater sets the next ID counter to the given value if it's greater than current.
func (s *Store) SetNextIDIfGreater(entityType string, newID int64) error {
	ai, ok := s.nextIDs[entityType]
	if !ok {
		// Read last ID from DB
		key := fmt.Sprintf("state:next_id:%s", entityType)
		data, _ := s.db.TurboRawRead(key)
		var lastID int64 = 0
		if len(data) > 0 {
			_, _ = fmt.Sscanf(string(data), "%d", &lastID)
		}
		ai = new(atomic.Int64)
		ai.Store(lastID)
		s.nextIDs[entityType] = ai
	}

	// Compare and swap until we succeed
	for {
		current := ai.Load()
		if newID <= current {
			return nil // Already higher, nothing to do
		}
		if ai.CompareAndSwap(current, newID) {
			key := fmt.Sprintf("state:next_id:%s", entityType)
			if err := s.db.TurboRawWrite(key, []byte(fmt.Sprintf("%d", newID))); err != nil {
				return err
			}
			return nil
		}
	}
}

// DocPut stores a document by key using TurboRawWrite.
func (s *Store) DocPut(key string, value []byte) error {
	if err := s.db.TurboRawWrite(key, value); err != nil {
		return fmt.Errorf("doc_put %s: %w", key, err)
	}
	return nil
}

// DocGet retrieves a document by key using TurboRawRead.
func (s *Store) DocGet(key string) ([]byte, error) {
	val, err := s.db.TurboRawRead(key)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if len(val) == 0 {
		return nil, ErrKeyNotFound
	}
	return val, nil
}

// DocDelete removes a document by key. Uses TurboRawWrite with empty value.
func (s *Store) DocDelete(key string) error {
	if err := s.db.TurboRawWrite(key, []byte{}); err != nil {
		return fmt.Errorf("doc_delete %s: %w", key, err)
	}
	return nil
}

// ParseInt64FromKey extracts an int64 ID from a key like "product:123".
func ParseInt64FromKey(key string, prefix string) (int64, bool) {
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return 0, false
	}
	id, err := strconv.ParseInt(key[len(prefix):], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
