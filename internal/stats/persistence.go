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
	// subsequent writes (updateReferrerStats/updatePathStats/updateUserAgentStats) don't panic.
	if stats.ReferrerStats == nil {
		stats.ReferrerStats = make(map[string]*ReferrerStats)
	}
	if stats.FullReferrerStats == nil {
		stats.FullReferrerStats = make(map[string]*FullReferrerStats)
	}
	if stats.PathStats == nil {
		stats.PathStats = make(map[int64]*PathStats)
	}
	if stats.UserAgentStats == nil {
		stats.UserAgentStats = make(map[string]*UserAgentStats)
	}
	// Excluded IPs are loaded from JSON directly — no init needed

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

// EventPersistence handles saving and loading detailed event data.
type EventPersistence struct {
	eventsKey       string
	dictionariesKey string
	botCountsKey    string
}

// NewEventPersistence creates a new EventPersistence instance.
func NewEventPersistence(prefix string) *EventPersistence {
	return &EventPersistence{
		eventsKey:       prefix + ":events",
		dictionariesKey: prefix + ":dictionaries",
		botCountsKey:    prefix + ":bot_counts",
	}
}

// SaveEvents saves detailed events to storage.
func (p *EventPersistence) SaveEvents(events []DetailedVisitEvent, store interface {
	TurboRawWrite(key string, value []byte) error
}) error {
	jsonData, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}
	return store.TurboRawWrite(p.eventsKey, jsonData)
}

// LoadEvents loads detailed events from storage.
func (p *EventPersistence) LoadEvents(store interface {
	TurboRawRead(key string) ([]byte, error)
}) ([]DetailedVisitEvent, error) {
	data, err := store.TurboRawRead(p.eventsKey)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	if len(data) == 0 {
		return []DetailedVisitEvent{}, nil
	}

	var events []DetailedVisitEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("unmarshal events: %w", err)
	}
	return events, nil
}

// SaveDictionaries saves UA and IP dictionaries to storage.
func (p *EventPersistence) SaveDictionaries(uaEntries, ipEntries []DictionaryEntry, store interface {
	TurboRawWrite(key string, value []byte) error
}) error {
	type DictData struct {
		UA []DictionaryEntry `json:"ua"`
		IP []DictionaryEntry `json:"ip"`
	}
	data := DictData{UA: uaEntries, IP: ipEntries}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal dictionaries: %w", err)
	}
	return store.TurboRawWrite(p.dictionariesKey, jsonData)
}

// LoadDictionaries loads UA and IP dictionaries from storage.
func (p *EventPersistence) LoadDictionaries(store interface {
	TurboRawRead(key string) ([]byte, error)
}) ([]DictionaryEntry, []DictionaryEntry, error) {
	data, err := store.TurboRawRead(p.dictionariesKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read dictionaries: %w", err)
	}
	if len(data) == 0 {
		return []DictionaryEntry{}, []DictionaryEntry{}, nil
	}

	type DictData struct {
		UA []DictionaryEntry `json:"ua"`
		IP []DictionaryEntry `json:"ip"`
	}
	var dictData DictData
	if err := json.Unmarshal(data, &dictData); err != nil {
		return nil, nil, fmt.Errorf("unmarshal dictionaries: %w", err)
	}
	return dictData.UA, dictData.IP, nil
}

// SaveBotCounts saves bot visit counts to storage.
func (p *EventPersistence) SaveBotCounts(counts map[string]uint64, store interface {
	TurboRawWrite(key string, value []byte) error
}) error {
	jsonData, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("marshal bot counts: %w", err)
	}
	return store.TurboRawWrite(p.botCountsKey, jsonData)
}

// LoadBotCounts loads bot visit counts from storage.
func (p *EventPersistence) LoadBotCounts(store interface {
	TurboRawRead(key string) ([]byte, error)
}) (map[string]uint64, error) {
	data, err := store.TurboRawRead(p.botCountsKey)
	if err != nil {
		return nil, fmt.Errorf("read bot counts: %w", err)
	}
	if len(data) == 0 {
		return map[string]uint64{}, nil
	}

	var counts map[string]uint64
	if err := json.Unmarshal(data, &counts); err != nil {
		return nil, fmt.Errorf("unmarshal bot counts: %w", err)
	}
	return counts, nil
}
