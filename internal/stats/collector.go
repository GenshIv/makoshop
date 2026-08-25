package stats

import (
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// StatsCollector collects and stores statistics
type StatsCollector struct {
	config    StatsConfig
	data      *StatsData
	VisitChan chan VisitEvent
	enabled   atomic.Bool
	dirty     atomic.Bool
	saveChan  chan struct{}

	// For persistence
	persistence *StatsPersistence
	store       interface {
		TurboRawWrite(key string, value []byte) error
		TurboRawRead(key string) ([]byte, error)
	}
}

// NewStatsCollector creates a new StatsCollector
func NewStatsCollector(config StatsConfig, visitChanSize int) *StatsCollector {
	return &StatsCollector{
		config:    config,
		data:      NewStatsData(),
		VisitChan: make(chan VisitEvent, visitChanSize),
		saveChan:  make(chan struct{}, 1),
	}
}

// NewStatsCollectorWithPersistence creates a new StatsCollector with persistence
func NewStatsCollectorWithPersistence(config StatsConfig, visitChanSize int, storeKey string, store interface {
	TurboRawWrite(key string, value []byte) error
	TurboRawRead(key string) ([]byte, error)
}) *StatsCollector {
	collector := &StatsCollector{
		config:      config,
		data:        NewStatsData(),
		VisitChan:   make(chan VisitEvent, visitChanSize),
		saveChan:    make(chan struct{}, 1),
		persistence: NewStatsPersistence(storeKey),
		store:       store,
	}
	return collector
}

// Start starts the stats collector
func (s *StatsCollector) Start() {
	s.enabled.Store(s.config.Enabled)
	go s.processVisits()
	go s.saveLoop()
}

// Stop stops the stats collector
func (s *StatsCollector) Stop() {
	s.enabled.Store(false)
	close(s.VisitChan)
}

// SetEnabled enables or disables stats collection
func (s *StatsCollector) SetEnabled(enabled bool) {
	s.enabled.Store(enabled)
}

// IsEnabled returns whether stats collection is enabled
func (s *StatsCollector) IsEnabled() bool {
	return s.enabled.Load()
}

// RecordVisit adds a visit event to the channel (non-blocking)
func (s *StatsCollector) RecordVisit(event VisitEvent) {
	if !s.enabled.Load() {
		return
	}

	select {
	case s.VisitChan <- event:
		// Event added successfully
	default:
		// Channel is full, skip this event (non-blocking)
	}
}

// processVisits processes visit events from the channel
func (s *StatsCollector) processVisits() {
	for event := range s.VisitChan {
		s.recordVisit(event)
	}
}

// recordVisit records a single visit
func (s *StatsCollector) recordVisit(event VisitEvent) {
	now := time.Now().UTC()
	hour := uint8(now.Hour())
	dayOfWeek := uint8(now.Weekday())
	dayOfMonth := uint8(now.Day())

	// Update hours
	if hour != s.data.CurrentHour {
		// Reset current hour
		s.data.HumanVisitsByHour[hour] = 0
		s.data.BotVisitsByHour[hour] = 0
		s.data.CurrentHour = hour
	}

	// Update days of week
	if dayOfWeek != s.data.CurrentDayOfWeek {
		// Reset current day of week
		s.data.HumanVisitsByDay[dayOfWeek] = 0
		s.data.BotVisitsByDay[dayOfWeek] = 0
		s.data.CurrentDayOfWeek = dayOfWeek
	}

	// Update days of month
	if dayOfMonth != s.data.CurrentDayOfMonth {
		// Reset current day of month
		s.data.HumanVisitsByMonthDay[dayOfMonth] = 0
		s.data.BotVisitsByMonthDay[dayOfMonth] = 0
		s.data.CurrentDayOfMonth = dayOfMonth
	}

	// Increment counters
	if event.IsBot {
		atomic.AddUint32(&s.data.BotVisitsByHour[hour], 1)
		atomic.AddUint32(&s.data.BotVisitsByDay[dayOfWeek], 1)
		atomic.AddUint32(&s.data.BotVisitsByMonthDay[dayOfMonth], 1)
	} else {
		atomic.AddUint32(&s.data.HumanVisitsByHour[hour], 1)
		atomic.AddUint32(&s.data.HumanVisitsByDay[dayOfWeek], 1)
		atomic.AddUint32(&s.data.HumanVisitsByMonthDay[dayOfMonth], 1)
	}

	// Update referrer stats
	if event.Referrer != "" {
		domain := extractDomain(event.Referrer)
		if domain != "" {
			s.updateReferrerStats(domain, hour)
		}
	}

	// Update path stats
	if event.CategoryID != 0 {
		s.updatePathStats(event.CategoryID, hour)
	}

	// Mark as dirty (needs saving)
	s.dirty.Store(true)
}

// updateReferrerStats updates referrer statistics
func (s *StatsCollector) updateReferrerStats(domain string, hour uint8) {
	stats, exists := s.data.ReferrerStats[domain]
	if !exists {
		stats = &ReferrerStats{
			Domain: domain,
		}
		s.data.ReferrerStats[domain] = stats
	}
	atomic.AddUint32(&stats.Visits[hour], 1)

	// Limit number of referrers
	if len(s.data.ReferrerStats) > s.config.MaxReferrers {
		s.cleanupReferrers()
	}
}

// updatePathStats updates path statistics
func (s *StatsCollector) updatePathStats(categoryID int64, hour uint8) {
	stats, exists := s.data.PathStats[categoryID]
	if !exists {
		stats = &PathStats{
			CategoryID: categoryID,
		}
		s.data.PathStats[categoryID] = stats
	}
	atomic.AddUint32(&stats.Visits[hour], 1)

	// Limit number of paths
	if len(s.data.PathStats) > s.config.MaxPaths {
		s.cleanupPaths()
	}
}

// cleanupReferrers removes least active referrers
func (s *StatsCollector) cleanupReferrers() {
	// Simple cleanup: remove referrers with 0 visits in current hour
	for domain, stats := range s.data.ReferrerStats {
		total := uint32(0)
		for i := 0; i < 24; i++ {
			total += stats.Visits[i]
		}
		if total == 0 {
			delete(s.data.ReferrerStats, domain)
		}
	}
}

// cleanupPaths removes least active paths
func (s *StatsCollector) cleanupPaths() {
	// Simple cleanup: remove paths with 0 visits in current hour
	for catID, stats := range s.data.PathStats {
		total := uint32(0)
		for i := 0; i < 24; i++ {
			total += stats.Visits[i]
		}
		if total == 0 {
			delete(s.data.PathStats, catID)
		}
	}
}

// saveLoop saves stats periodically
func (s *StatsCollector) saveLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		// Save only if there are changes
		if s.dirty.CompareAndSwap(true, false) {
			s.saveToDB()
		}
	}
}

// saveToDB saves stats to database
func (s *StatsCollector) saveToDB() {
	if s.persistence != nil && s.store != nil {
		if err := s.persistence.SaveStatsData(s.data, s.store); err != nil {
			fmt.Printf("WARN: failed to save stats: %v\n", err)
		}
	}
}

// GetSummary returns summary statistics
func (s *StatsCollector) GetSummary() Summary {
	return s.data.GetSummary()
}

// GetReferrers returns referrer statistics
func (s *StatsCollector) GetReferrers() map[string]*ReferrerStats {
	return s.data.GetReferrers()
}

// GetPaths returns path statistics
func (s *StatsCollector) GetPaths() map[int64]*PathStats {
	return s.data.GetPaths()
}

// extractDomain extracts domain from referrer URL
func extractDomain(referrer string) string {
	if referrer == "" {
		return ""
	}

	u, err := url.Parse(referrer)
	if err != nil {
		return ""
	}

	host := u.Host
	if host == "" {
		return ""
	}

	// Remove port if present
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}

	return host
}
