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
			// Restore the in-memory excluded-IP list from the persisted data.
			// IsIPExcluded/GetExcludedIPs read s.excludedIPs (not s.data.ExcludedIPs),
			// so without this the exclusion list would be silently dropped on restart.
			s.excludedIPs = s.data.ExcludedIPs
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

// rotatePeriods aligns the stored "current" markers to the real UTC time.
//
// The bucket arrays are indexed by clock value: slot h of the hour carousel
// holds the visits of the most recent UTC hour h (likewise slot d of the
// day carousel holds the most recent weekday d, and slot m of the month-day
// carousel holds the most recent calendar day m+1). recordVisit always
// increments the slot of the current clock value, and the UI reads the
// arrays back with the same indexing, so rotation must NOT shift the
// arrays — shifting would move every bucket into the wrong slot and merge
// the previous period's count into the current one.
//
// On startup (or after a gap) a marker may lag behind real time. We advance
// each marker one step at a time, zeroing the slot that just started again:
// it still holds data from one full period ago (24h / 7d / 31d) and must be
// replaced with a fresh counter. Once marker == real, calls are no-ops.
func (s *StatsCollector) rotatePeriods(hour, dayOfWeek, dayOfMonth uint8) {
	// Hour carousel (24h sliding window)
	for s.data.CurrentHour != hour {
		next := (s.data.CurrentHour + 1) % 24
		s.data.HumanVisitsByHour[next] = 0
		s.data.BotVisitsByHour[next] = 0
		// Per-entity carousels (referrers, paths, user agents) share the same
		// clock-hour indexing, so their just-started hour slot must be reset
		// too, otherwise the new hour accumulates on top of 24h-old data.
		s.zeroEntityHourSlot(next)
		s.data.CurrentHour = next
	}

	// Day-of-week carousel (7d sliding window)
	for s.data.CurrentDayOfWeek != dayOfWeek {
		next := (s.data.CurrentDayOfWeek + 1) % 7
		s.data.HumanVisitsByDay[next] = 0
		s.data.BotVisitsByDay[next] = 0
		s.data.CurrentDayOfWeek = next
	}

	// Day-of-month carousel (31d sliding window)
	for s.data.CurrentDayOfMonth != dayOfMonth {
		next := (s.data.CurrentDayOfMonth + 1) % 31
		s.data.HumanVisitsByMonthDay[next] = 0
		s.data.BotVisitsByMonthDay[next] = 0
		s.data.CurrentDayOfMonth = next
	}
}

// zeroEntityHourSlot resets the hour slot that just started for every
// per-entity carousel (referrers, full referrers, paths, user agents).
// Called from rotatePeriods as the hour marker advances.
func (s *StatsCollector) zeroEntityHourSlot(hour uint8) {
	for _, r := range s.data.ReferrerStats {
		r.Visits[hour] = 0
	}
	for _, f := range s.data.FullReferrerStats {
		f.Visits[hour] = 0
	}
	for _, p := range s.data.PathStats {
		p.Visits[hour] = 0
	}
	for _, u := range s.data.UserAgentStats {
		u.Visits[hour] = 0
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
