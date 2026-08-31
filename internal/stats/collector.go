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

	// Excluded IPs cache (loaded from config)
	excludedIPs []string
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

// Start starts the stats collector.
// It first loads any previously persisted data so statistics survive restarts,
// then eagerly rotates to the current period (zeroing only the just-started
// bucket) so a fresh day/hour is handled even before the first visit arrives.
func (s *StatsCollector) Start() {
	// Load persisted stats if available; fall back to fresh data on any error.
	if s.persistence != nil && s.store != nil {
		if loaded, err := s.persistence.LoadStatsData(s.store); err == nil && loaded != nil {
			s.data = loaded
		} else if err != nil {
			fmt.Printf("WARN: failed to load stats: %v\n", err)
		}
	}

	// Reset only the period that just started (e.g. new hour/day), never all data.
	hour, dayOfWeek, dayOfMonth := s.currentPeriodIndices()
	s.rotatePeriods(hour, dayOfWeek, dayOfMonth)

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

// SetExcludedIPs updates the list of excluded IPs and marks dirty for save
func (s *StatsCollector) SetExcludedIPs(ips []string) {
	s.excludedIPs = ips
	s.data.ExcludedIPs = ips
	s.dirty.Store(true)
}

// GetExcludedIPs returns the list of excluded IPs
func (s *StatsCollector) GetExcludedIPs() []string {
	return s.excludedIPs
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
	// Check if IP is excluded
	if IsIPExcluded(event.IP, s.excludedIPs) {
		return
	}

	hour, dayOfWeek, dayOfMonth := s.currentPeriodIndices()

	// Reset only the period that just started; all other periods are preserved.
	s.rotatePeriods(hour, dayOfWeek, dayOfMonth)

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

	// Update user agent stats
	if event.UserAgent != "" {
		s.updateUserAgentStats(event.UserAgent, hour, event.IsBot)
	}

	// Mark as dirty (needs saving)
	s.dirty.Store(true)
}

// currentPeriodIndices returns the current hour, weekday and 0-based day-of-month.
// Day-of-month is stored 0-based (now.Day()-1) so it indexes [31] arrays safely:
// using now.Day() directly would overflow to index 31 on the 31st and panic.
func (s *StatsCollector) currentPeriodIndices() (hour, dayOfWeek, dayOfMonth uint8) {
	now := time.Now().UTC()
	hour = uint8(now.Hour())
	dayOfWeek = uint8(now.Weekday())
	dayOfMonth = uint8(now.Day() - 1)
	return
}

// rotatePeriods aligns the stored "current" marker to the real time.
// On startup (or after a gap) the stored hour/day/month may lag behind
// real time. We rotate the arrays forward until the marker catches up,
// zeroing each newly-reached slot so fresh counting starts clean.
// Once marker == real, subsequent calls are no-ops.
func (s *StatsCollector) rotatePeriods(hour, dayOfWeek, dayOfMonth uint8) {
	// Hour carousel (24h sliding window)
	for s.data.CurrentHour != hour {
		// Shift all hours forward by one, drop the oldest
		for i := 23; i > 0; i-- {
			s.data.HumanVisitsByHour[i] = s.data.HumanVisitsByHour[i-1]
			s.data.BotVisitsByHour[i] = s.data.BotVisitsByHour[i-1]
		}
		s.data.HumanVisitsByHour[0] = 0
		s.data.BotVisitsByHour[0] = 0
		s.data.CurrentHour = (s.data.CurrentHour + 1) % 24
	}

	// Day-of-week carousel (7d sliding window)
	for s.data.CurrentDayOfWeek != dayOfWeek {
		for i := 6; i > 0; i-- {
			s.data.HumanVisitsByDay[i] = s.data.HumanVisitsByDay[i-1]
			s.data.BotVisitsByDay[i] = s.data.BotVisitsByDay[i-1]
		}
		s.data.HumanVisitsByDay[0] = 0
		s.data.BotVisitsByDay[0] = 0
		s.data.CurrentDayOfWeek = (s.data.CurrentDayOfWeek + 1) % 7
	}

	// Day-of-month carousel (31d sliding window)
	for s.data.CurrentDayOfMonth != dayOfMonth {
		for i := 30; i > 0; i-- {
			s.data.HumanVisitsByMonthDay[i] = s.data.HumanVisitsByMonthDay[i-1]
			s.data.BotVisitsByMonthDay[i] = s.data.BotVisitsByMonthDay[i-1]
		}
		s.data.HumanVisitsByMonthDay[0] = 0
		s.data.BotVisitsByMonthDay[0] = 0
		s.data.CurrentDayOfMonth = (s.data.CurrentDayOfMonth + 1) % 31
	}
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

// updateUserAgentStats updates user agent statistics
func (s *StatsCollector) updateUserAgentStats(ua string, hour uint8, isBot bool) {
	browser := classifyUserAgent(ua)
	if browser == "" {
		return
	}

	stats, exists := s.data.UserAgentStats[browser]
	if !exists {
		stats = &UserAgentStats{
			Browser: browser,
		}
		s.data.UserAgentStats[browser] = stats
	}
	atomic.AddUint32(&stats.Visits[hour], 1)
}

// classifyUserAgent classifies user agent into browser category
func classifyUserAgent(ua string) string {
	if ua == "" {
		return ""
	}

	uaLower := strings.ToLower(ua)

	// Check for bots first
	if strings.Contains(uaLower, "bot") || strings.Contains(uaLower, "spider") || strings.Contains(uaLower, "crawler") {
		return "Bot"
	}

	// Check for specific browsers
	if strings.Contains(uaLower, "firefox") {
		return "Firefox"
	}
	if strings.Contains(uaLower, "chrome") && !strings.Contains(uaLower, "edg") {
		return "Chrome"
	}
	if strings.Contains(uaLower, "edg") || strings.Contains(uaLower, "edge") {
		return "Edge"
	}
	if strings.Contains(uaLower, "safari") && !strings.Contains(uaLower, "chrome") {
		return "Safari"
	}
	if strings.Contains(uaLower, "opera") || strings.Contains(uaLower, "opr") {
		return "Opera"
	}
	if strings.Contains(uaLower, "msie") || strings.Contains(uaLower, "trident") {
		return "IE"
	}

	return "Other"
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

// GetUserAgents returns user agent statistics
func (s *StatsCollector) GetUserAgents() map[string]*UserAgentStats {
	return s.data.GetUserAgents()
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
