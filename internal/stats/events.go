package stats

import (
	"sync"
	"time"
)

// DetailedVisitEvent represents a detailed visit event with dictionary IDs.
type DetailedVisitEvent struct {
	Timestamp uint32 // Unix timestamp
	Page      string // URL path
	Referer   string // Referrer URL (full)
	UAID      uint32 // User-Agent dictionary ID
	IPID      uint32 // IP dictionary ID
	SourceID  uint16 // Traffic source ID (0=direct, 1=search, 2=social, etc.)
	IsBot     bool
	BotName   string // Bot name if IsBot is true
}

// DictionaryEntry represents an entry in a string dictionary.
type DictionaryEntry struct {
	ID    uint32
	Value string
}

// StringDictionary maps strings to IDs and back.
type StringDictionary struct {
	mu      sync.RWMutex
	values  map[string]uint32 // string -> ID
	byID    map[uint32]string // ID -> string
	nextID  uint32
	maxSize int
}

// NewStringDictionary creates a new string dictionary.
func NewStringDictionary(maxSize int) *StringDictionary {
	return &StringDictionary{
		values:  make(map[string]uint32),
		byID:    make(map[uint32]string),
		nextID:  1, // Start at 1, 0 is reserved for "unknown"
		maxSize: maxSize,
	}
}

// GetOrAdd returns the ID for a string, adding it if necessary.
func (d *StringDictionary) GetOrAdd(value string) uint32 {
	if value == "" {
		return 0
	}

	d.mu.RLock()
	if id, ok := d.values[value]; ok {
		d.mu.RUnlock()
		return id
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock
	if id, ok := d.values[value]; ok {
		return id
	}

	id := d.nextID
	d.nextID++
	d.values[value] = id
	d.byID[id] = value

	// Evict oldest entries if over capacity (simple: clear all)
	if len(d.values) > d.maxSize {
		d.clear()
	}

	return id
}

// GetByID returns the string for an ID.
func (d *StringDictionary) GetByID(id uint32) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if id == 0 {
		return "unknown"
	}
	return d.byID[id]
}

// Entries returns all dictionary entries.
func (d *StringDictionary) Entries() []DictionaryEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entries := make([]DictionaryEntry, 0, len(d.byID))
	for id, value := range d.byID {
		entries = append(entries, DictionaryEntry{ID: id, Value: value})
	}
	return entries
}

// Size returns the number of entries.
func (d *StringDictionary) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.values)
}

func (d *StringDictionary) clear() {
	d.values = make(map[string]uint32)
	d.byID = make(map[uint32]string)
	d.nextID = 1
}

// EventStore stores visit events and dictionaries.
type EventStore struct {
	mu        sync.RWMutex
	events    []DetailedVisitEvent
	uaDict    *StringDictionary
	ipDict    *StringDictionary
	botCounts map[string]uint64 // bot name -> count
	maxEvents int
}

// NewEventStore creates a new event store.
func NewEventStore(maxEvents, maxUAEntries, maxIPEntries int) *EventStore {
	return &EventStore{
		events:    make([]DetailedVisitEvent, 0, maxEvents),
		uaDict:    NewStringDictionary(maxUAEntries),
		ipDict:    NewStringDictionary(maxIPEntries),
		botCounts: make(map[string]uint64),
		maxEvents: maxEvents,
	}
}

// StoreEvent stores a visit event. For bots, only increments the bot counter.
// For humans, stores the full event with dictionary IDs.
func (s *EventStore) StoreEvent(event DetailedVisitEvent, userAgent string, ip string) {
	if event.IsBot {
		s.incrementBotCount(event.BotName)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Resolve dictionary IDs
	event.UAID = s.uaDict.GetOrAdd(userAgent)
	event.IPID = s.ipDict.GetOrAdd(ip)

	// Store event
	s.events = append(s.events, DetailedVisitEvent{
		Timestamp: event.Timestamp,
		Page:      event.Page,
		Referer:   event.Referer,
		UAID:      event.UAID,
		IPID:      event.IPID,
		SourceID:  event.SourceID,
		IsBot:     false,
	})

	// Evict oldest events if over capacity
	if len(s.events) > s.maxEvents {
		overflow := len(s.events) - s.maxEvents
		s.events = s.events[overflow:]
	}
}

func (s *EventStore) incrementBotCount(botName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if botName == "" {
		botName = "unknown"
	}
	s.botCounts[botName]++
}

// GetEvents returns all stored events.
func (s *EventStore) GetEvents() []DetailedVisitEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DetailedVisitEvent, len(s.events))
	copy(result, s.events)
	return result
}

// GetUAEntries returns all user-agent dictionary entries.
func (s *EventStore) GetUAEntries() []DictionaryEntry {
	return s.uaDict.Entries()
}

// GetIPEntries returns all IP dictionary entries.
func (s *EventStore) GetIPEntries() []DictionaryEntry {
	return s.ipDict.Entries()
}

// GetBotCounts returns bot visit counts by name.
func (s *EventStore) GetBotCounts() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]uint64, len(s.botCounts))
	for k, v := range s.botCounts {
		result[k] = v
	}
	return result
}

// AggregateByPage returns visit counts grouped by page.
func (s *EventStore) AggregateByPage() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]uint64)
	for _, event := range s.events {
		result[event.Page]++
	}
	return result
}

// AggregateByReferer returns visit counts grouped by referer.
func (s *EventStore) AggregateByReferer() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]uint64)
	for _, event := range s.events {
		if event.Referer != "" {
			result[event.Referer]++
		}
	}
	return result
}

// AggregateByUA returns visit counts grouped by user-agent ID.
func (s *EventStore) AggregateByUA() map[uint32]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[uint32]uint64)
	for _, event := range s.events {
		result[event.UAID]++
	}
	return result
}

// AggregateByIP returns visit counts grouped by IP ID.
func (s *EventStore) AggregateByIP() map[uint32]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[uint32]uint64)
	for _, event := range s.events {
		result[event.IPID]++
	}
	return result
}

// AggregateByTime returns visit counts by hour of day.
func (s *EventStore) AggregateByTime() [24]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result [24]uint64
	for _, event := range s.events {
		t := time.Unix(int64(event.Timestamp), 0).UTC()
		result[t.Hour()]++
	}
	return result
}

// EventCount returns the number of stored events.
func (s *EventStore) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}
