package stats

import (
	"encoding/json"
	"fmt"
	"time"
)

// StatsPersistence handles saving and loading stats to/from storage
type StatsPersistence struct {
	storeKey string
}

// NewStatsPersistence creates a new StatsPersistence
func NewStatsPersistence(storeKey string) *StatsPersistence {
	return &StatsPersistence{
		storeKey: storeKey,
	}
}

// SaveStatsData saves stats data to storage
func (p *StatsPersistence) SaveStatsData(data *StatsData, store interface {
	TurboRawWrite(key string, value []byte) error
}) error {
	// Marshal stats data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal stats: %w", err)
	}

	// Save to store
	if err := store.TurboRawWrite(p.storeKey, jsonData); err != nil {
		return fmt.Errorf("write stats: %w", err)
	}

	return nil
}

// LoadStatsData loads stats data from storage
func (p *StatsPersistence) LoadStatsData(store interface {
	TurboRawWrite(key string, value []byte) error
	TurboRawRead(key string) ([]byte, error)
}) (*StatsData, error) {
	data, err := store.TurboRawRead(p.storeKey)
	if err != nil {
		return nil, fmt.Errorf("read stats: %w", err)
	}

	if len(data) == 0 {
		return NewStatsData(), nil
	}

	var stats StatsData
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}

	// JSON may omit or null the maps; ensure they are non-nil so that
	// subsequent writes (updateReferrerStats/updatePathStats) don't panic.
	if stats.ReferrerStats == nil {
		stats.ReferrerStats = make(map[string]*ReferrerStats)
	}
	if stats.FullReferrerStats == nil {
		stats.FullReferrerStats = make(map[string]*FullReferrerStats)
	}
	if stats.PathStats == nil {
		stats.PathStats = make(map[int64]*PathStats)
	}

	return &stats, nil
}

// StatsDataWithMeta is StatsData with metadata for persistence
type StatsDataWithMeta struct {
	Data      *StatsData `json:"data"`
	UpdatedAt time.Time  `json:"updated_at"`
	Version   int        `json:"version"`
}

// SaveStatsDataWithMeta saves stats data with metadata
func (p *StatsPersistence) SaveStatsDataWithMeta(data *StatsData, store interface {
	TurboRawWrite(key string, value []byte) error
}) error {
	meta := &StatsDataWithMeta{
		Data:      data,
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	}

	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal stats meta: %w", err)
	}

	if err := store.TurboRawWrite(p.storeKey, jsonData); err != nil {
		return fmt.Errorf("write stats meta: %w", err)
	}

	return nil
}

// LoadStatsDataWithMeta loads stats data with metadata
func (p *StatsPersistence) LoadStatsDataWithMeta(store interface {
	TurboRawWrite(key string, value []byte) error
	TurboRawRead(key string) ([]byte, error)
}) (*StatsDataWithMeta, error) {
	data, err := store.TurboRawRead(p.storeKey)
	if err != nil {
		return nil, fmt.Errorf("read stats meta: %w", err)
	}

	if len(data) == 0 {
		return &StatsDataWithMeta{
			Data:      NewStatsData(),
			UpdatedAt: time.Now().UTC(),
			Version:   1,
		}, nil
	}

	var meta StatsDataWithMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal stats meta: %w", err)
	}

	if meta.Data == nil {
		meta.Data = NewStatsData()
	}

	return &meta, nil
}
