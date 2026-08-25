package stats

import (
	"time"
)

// VisitEvent represents a single visit event
type VisitEvent struct {
	IsBot      bool
	Referrer   string
	CategoryID int64
	Timestamp  uint32
}

// ReferrerStats tracks visits by referrer domain
type ReferrerStats struct {
	Domain string
	Visits [24]uint32 // by hour
}

// FullReferrerStats tracks visits by full referrer URL (for specific systems like Google)
type FullReferrerStats struct {
	FullReferrer string
	Visits       [24]uint32 // by hour
}

// PathStats tracks visits by category ID
type PathStats struct {
	CategoryID int64
	Visits     [24]uint32 // by hour
}

// StatsData holds all statistics data
type StatsData struct {
	// Hours (0-23)
	CurrentHour       uint8
	HumanVisitsByHour [24]uint32
	BotVisitsByHour   [24]uint32

	// Days of week (0-6, Sunday=0)
	CurrentDayOfWeek uint8
	HumanVisitsByDay [7]uint32
	BotVisitsByDay   [7]uint32

	// Days of month (1-31)
	CurrentDayOfMonth     uint8
	HumanVisitsByMonthDay [31]uint32
	BotVisitsByMonthDay   [31]uint32

	// Referrer stats (by domain)
	ReferrerStats map[string]*ReferrerStats

	// Full referrer stats (for specific systems, e.g., Google)
	FullReferrerStats map[string]*FullReferrerStats

	// Path stats (by category ID)
	PathStats map[int64]*PathStats
}

// NewStatsData creates a new StatsData instance
func NewStatsData() *StatsData {
	return &StatsData{
		ReferrerStats:     make(map[string]*ReferrerStats),
		FullReferrerStats: make(map[string]*FullReferrerStats),
		PathStats:         make(map[int64]*PathStats),
	}
}

// StatsConfig holds configuration for stats collection
type StatsConfig struct {
	Enabled           bool
	MaxReferrers      int
	MaxPaths          int
	TrackFullReferrer bool
	GoogleReferrers   []string // full referrer patterns to track
}

// DefaultStatsConfig returns default configuration
func DefaultStatsConfig() StatsConfig {
	return StatsConfig{
		Enabled:           true, // enabled by default
		MaxReferrers:      100,
		MaxPaths:          100,
		TrackFullReferrer: false,
		GoogleReferrers:   []string{"google.com", "google.ru", "google.co.uk"},
	}
}

// Summary represents the summary statistics
type Summary struct {
	TotalHumanVisits      uint64
	TotalBotVisits        uint64
	HumanVisitsByHour     [24]uint32
	BotVisitsByHour       [24]uint32
	HumanVisitsByDay      [7]uint32
	BotVisitsByDay        [7]uint32
	HumanVisitsByMonthDay [31]uint32
	BotVisitsByMonthDay   [31]uint32
}

// GetSummary returns summary statistics
func (s *StatsData) GetSummary() Summary {
	var summary Summary

	// Sum all hours
	for i := 0; i < 24; i++ {
		summary.TotalHumanVisits += uint64(s.HumanVisitsByHour[i])
		summary.TotalBotVisits += uint64(s.BotVisitsByHour[i])
		summary.HumanVisitsByHour[i] = s.HumanVisitsByHour[i]
		summary.BotVisitsByHour[i] = s.BotVisitsByHour[i]
	}

	// Sum all days of week
	for i := 0; i < 7; i++ {
		summary.HumanVisitsByDay[i] = s.HumanVisitsByDay[i]
		summary.BotVisitsByDay[i] = s.BotVisitsByDay[i]
	}

	// Sum all days of month
	for i := 0; i < 31; i++ {
		summary.HumanVisitsByMonthDay[i] = s.HumanVisitsByMonthDay[i]
		summary.BotVisitsByMonthDay[i] = s.BotVisitsByMonthDay[i]
	}

	return summary
}

// GetReferrers returns referrer statistics
func (s *StatsData) GetReferrers() map[string]*ReferrerStats {
	return s.ReferrerStats
}

// GetPaths returns path statistics
func (s *StatsData) GetPaths() map[int64]*PathStats {
	return s.PathStats
}

// GetCurrentTime returns current UTC time components
func GetCurrentTime() (hour uint8, dayOfWeek uint8, dayOfMonth uint8) {
	now := time.Now().UTC()
	return uint8(now.Hour()), uint8(now.Weekday()), uint8(now.Day())
}
