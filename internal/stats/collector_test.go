package stats

import (
	"fmt"
	"testing"
)

// memStore is an in-memory implementation of the raw store interface used
// by the stats persistence layer.
type memStore struct {
	m map[string][]byte
}

func (s *memStore) TurboRawWrite(key string, value []byte) error {
	if s.m == nil {
		s.m = make(map[string][]byte)
	}
	s.m[key] = value
	return nil
}

func (s *memStore) TurboRawRead(key string) ([]byte, error) {
	v, ok := s.m[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return v, nil
}

// TestRotatePeriodsHourBoundaryPreservesData verifies that when the first
// visit of a new hour arrives, only the just-started hour slot is zeroed and
// all previously recorded hour buckets keep their values in their own slots.
//
// The arrays are indexed by clock hour: slot h holds the visits of the most
// recent UTC hour h. A rotation must NOT shift the array, otherwise every
// bucket moves one slot and the new current hour accumulates the previous
// hour's count.
func TestRotatePeriodsHourBoundaryPreservesData(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	// Server was last active at UTC hour 10 with data in the recent slots.
	c.data.CurrentHour = 10
	c.data.HumanVisitsByHour[10] = 5 // live hour (in progress)
	c.data.HumanVisitsByHour[9] = 4  // previous hour
	c.data.HumanVisitsByHour[8] = 9  // two hours ago
	c.data.BotVisitsByHour[9] = 2

	// It is now UTC hour 11 and the first visit of the new hour triggers rotation.
	c.rotatePeriods(11, c.data.CurrentDayOfWeek, c.data.CurrentDayOfMonth)

	if c.data.CurrentHour != 11 {
		t.Fatalf("CurrentHour = %d, want 11", c.data.CurrentHour)
	}
	if got := c.data.HumanVisitsByHour[11]; got != 0 {
		t.Errorf("slot 11 (just started) = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByHour[10]; got != 5 {
		t.Errorf("slot 10 = %d, want 5 (hour 10 data must stay in slot 10)", got)
	}
	if got := c.data.HumanVisitsByHour[9]; got != 4 {
		t.Errorf("slot 9 = %d, want 4", got)
	}
	if got := c.data.HumanVisitsByHour[8]; got != 9 {
		t.Errorf("slot 8 = %d, want 9", got)
	}
	if got := c.data.BotVisitsByHour[9]; got != 2 {
		t.Errorf("bot slot 9 = %d, want 2", got)
	}
}

// TestRotatePeriodsGapZerosAllSkippedSlots verifies that after a gap (e.g.
// server restart) every hour slot that has started since the last marker is
// zeroed, while older data is preserved.
func TestRotatePeriodsGapZerosAllSkippedSlots(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	c.data.CurrentHour = 5
	c.data.HumanVisitsByHour[5] = 10 // live hour
	c.data.HumanVisitsByHour[4] = 7  // previous hour
	c.data.HumanVisitsByHour[6] = 3  // stale: from yesterday, must be zeroed
	c.data.HumanVisitsByHour[7] = 1  // stale: from yesterday, must be zeroed

	// Server was down from hour 6 to hour 7 (now it is hour 7).
	c.rotatePeriods(7, c.data.CurrentDayOfWeek, c.data.CurrentDayOfMonth)

	if c.data.CurrentHour != 7 {
		t.Fatalf("CurrentHour = %d, want 7", c.data.CurrentHour)
	}
	if got := c.data.HumanVisitsByHour[6]; got != 0 {
		t.Errorf("slot 6 (skipped hour) = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByHour[7]; got != 0 {
		t.Errorf("slot 7 (current hour) = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByHour[5]; got != 10 {
		t.Errorf("slot 5 = %d, want 10", got)
	}
	if got := c.data.HumanVisitsByHour[4]; got != 7 {
		t.Errorf("slot 4 = %d, want 7", got)
	}
}

// TestRotatePeriodsWraparound verifies rotation across the 23 -> 0 -> 1 boundary.
func TestRotatePeriodsWraparound(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	c.data.CurrentHour = 23
	c.data.HumanVisitsByHour[23] = 8 // live hour
	c.data.HumanVisitsByHour[0] = 2  // stale: from yesterday
	c.data.HumanVisitsByHour[1] = 4  // stale: from yesterday

	// Now it is hour 1 (crossed midnight).
	c.rotatePeriods(1, c.data.CurrentDayOfWeek, c.data.CurrentDayOfMonth)

	if c.data.CurrentHour != 1 {
		t.Fatalf("CurrentHour = %d, want 1", c.data.CurrentHour)
	}
	if got := c.data.HumanVisitsByHour[0]; got != 0 {
		t.Errorf("slot 0 = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByHour[1]; got != 0 {
		t.Errorf("slot 1 = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByHour[23]; got != 8 {
		t.Errorf("slot 23 = %d, want 8", got)
	}
}

// TestRotatePeriodsNoOpWhenAligned verifies that rotation is a no-op when the
// marker already matches the real time.
func TestRotatePeriodsNoOpWhenAligned(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	c.data.CurrentHour = 12
	c.data.HumanVisitsByHour[12] = 6

	c.rotatePeriods(12, c.data.CurrentDayOfWeek, c.data.CurrentDayOfMonth)

	if got := c.data.HumanVisitsByHour[12]; got != 6 {
		t.Errorf("slot 12 = %d, want 6 (aligned rotation must not touch data)", got)
	}
}

// TestRotatePeriodsDayOfWeekSameSemantics verifies the day-of-week carousel
// uses the same zero-skipped-slots semantics.
func TestRotatePeriodsDayOfWeekSameSemantics(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	c.data.CurrentDayOfWeek = 2 // Wednesday
	c.data.HumanVisitsByDay[2] = 5
	c.data.HumanVisitsByDay[3] = 1 // stale: from last Thursday

	// Now it is Friday (5): Thursday and Friday slots must be zeroed.
	c.rotatePeriods(c.data.CurrentHour, 5, c.data.CurrentDayOfMonth)

	if c.data.CurrentDayOfWeek != 5 {
		t.Fatalf("CurrentDayOfWeek = %d, want 5", c.data.CurrentDayOfWeek)
	}
	if got := c.data.HumanVisitsByDay[3]; got != 0 {
		t.Errorf("day slot 3 = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByDay[5]; got != 0 {
		t.Errorf("day slot 5 = %d, want 0", got)
	}
	if got := c.data.HumanVisitsByDay[2]; got != 5 {
		t.Errorf("day slot 2 = %d, want 5", got)
	}
}

// TestStartRestoresExcludedIPs verifies that the in-memory excluded-IP list is
// restored from persisted data on Start, so exclusions survive a restart.
func TestStartRestoresExcludedIPs(t *testing.T) {
	store := &memStore{}
	p := NewStatsPersistence("stats:visits")

	data := NewStatsData()
	data.ExcludedIPs = []string{"83.175.187.114", "10.0.0.5"}
	if err := p.SaveStatsData(data, store); err != nil {
		t.Fatalf("SaveStatsData: %v", err)
	}

	c := NewStatsCollectorWithPersistence(DefaultStatsConfig(), 10, "stats:visits", store)
	c.Start()

	got := c.GetExcludedIPs()
	if len(got) != 2 || got[0] != "83.175.187.114" || got[1] != "10.0.0.5" {
		t.Fatalf("GetExcludedIPs after Start = %v, want [83.175.187.114 10.0.0.5]", got)
	}
	if !IsIPExcluded("83.175.187.114", c.GetExcludedIPs()) {
		t.Errorf("IsIPExcluded(83.175.187.114) = false, want true after restart")
	}
	if IsIPExcluded("1.2.3.4", c.GetExcludedIPs()) {
		t.Errorf("IsIPExcluded(1.2.3.4) = true, want false")
	}
}

// TestRecordVisitIncrementsCurrentUTCHourSlot verifies that a recorded visit
// lands in the slot of the current UTC hour and nowhere else.
func TestRecordVisitIncrementsCurrentUTCHourSlot(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)
	c.data.CurrentHour = 255 // force a different value so Start() rotation runs
	c.Start()

	hour, _, _ := c.currentPeriodIndices()
	if c.data.CurrentHour != hour {
		t.Fatalf("after Start, CurrentHour = %d, want %d", c.data.CurrentHour, hour)
	}

	c.recordVisit(VisitEvent{IsBot: false, UserAgent: "Mozilla/5.0 Chrome"})

	if got := c.data.HumanVisitsByHour[hour]; got != 1 {
		t.Errorf("slot %d = %d, want 1", hour, got)
	}
	for i := 0; i < 24; i++ {
		if i == int(hour) {
			continue
		}
		if got := c.data.HumanVisitsByHour[i]; got != 0 {
			t.Errorf("slot %d = %d, want 0 (visit must only touch the current hour slot)", i, got)
		}
	}
}

// TestRotatePeriodsZerosEntityHourSlots verifies that per-entity carousels
// (referrers, paths, user agents) reset their just-started hour slot on
// rotation, while other hour slots are preserved.
func TestRotatePeriodsZerosEntityHourSlots(t *testing.T) {
	c := NewStatsCollector(DefaultStatsConfig(), 10)

	c.data.CurrentHour = 10
	ref := &ReferrerStats{Domain: "example.com"}
	ref.Visits[11] = 4 // stale: from yesterday's hour 11
	ref.Visits[9] = 6
	c.data.ReferrerStats["example.com"] = ref

	path := &PathStats{CategoryID: 42}
	path.Visits[11] = 2
	path.Visits[8] = 7
	c.data.PathStats[42] = path

	ua := &UserAgentStats{Browser: "Chrome"}
	ua.Visits[11] = 3
	ua.Visits[7] = 11
	c.data.UserAgentStats["Chrome"] = ua

	full := &FullReferrerStats{FullReferrer: "https://example.com/x"}
	full.Visits[11] = 5
	c.data.FullReferrerStats["https://example.com/x"] = full

	// Now it is hour 11.
	c.rotatePeriods(11, c.data.CurrentDayOfWeek, c.data.CurrentDayOfMonth)

	if got := ref.Visits[11]; got != 0 {
		t.Errorf("referrer slot 11 = %d, want 0", got)
	}
	if got := ref.Visits[9]; got != 6 {
		t.Errorf("referrer slot 9 = %d, want 6", got)
	}
	if got := path.Visits[11]; got != 0 {
		t.Errorf("path slot 11 = %d, want 0", got)
	}
	if got := path.Visits[8]; got != 7 {
		t.Errorf("path slot 8 = %d, want 7", got)
	}
	if got := ua.Visits[11]; got != 0 {
		t.Errorf("user agent slot 11 = %d, want 0", got)
	}
	if got := ua.Visits[7]; got != 11 {
		t.Errorf("user agent slot 7 = %d, want 11", got)
	}
	if got := full.Visits[11]; got != 0 {
		t.Errorf("full referrer slot 11 = %d, want 0", got)
	}
}

// TestPersistenceRoundTrip verifies that stats data (including the period
// markers) survives a save/load cycle intact.
func TestPersistenceRoundTrip(t *testing.T) {
	store := &memStore{}
	p := NewStatsPersistence("stats:visits")

	data := NewStatsData()
	data.CurrentHour = 3
	data.CurrentDayOfWeek = 4
	data.CurrentDayOfMonth = 10
	data.HumanVisitsByHour[3] = 42
	data.BotVisitsByHour[5] = 7
	data.HumanVisitsByDay[4] = 99
	data.ReferrerStats["example.com"] = &ReferrerStats{Domain: "example.com"}
	data.ReferrerStats["example.com"].Visits[3] = 5

	if err := p.SaveStatsData(data, store); err != nil {
		t.Fatalf("SaveStatsData: %v", err)
	}

	loaded, err := p.LoadStatsData(store)
	if err != nil {
		t.Fatalf("LoadStatsData: %v", err)
	}

	if loaded.CurrentHour != 3 {
		t.Errorf("CurrentHour = %d, want 3", loaded.CurrentHour)
	}
	if loaded.CurrentDayOfWeek != 4 {
		t.Errorf("CurrentDayOfWeek = %d, want 4", loaded.CurrentDayOfWeek)
	}
	if loaded.CurrentDayOfMonth != 10 {
		t.Errorf("CurrentDayOfMonth = %d, want 10", loaded.CurrentDayOfMonth)
	}
	if got := loaded.HumanVisitsByHour[3]; got != 42 {
		t.Errorf("HumanVisitsByHour[3] = %d, want 42", got)
	}
	if got := loaded.BotVisitsByHour[5]; got != 7 {
		t.Errorf("BotVisitsByHour[5] = %d, want 7", got)
	}
	if got := loaded.HumanVisitsByDay[4]; got != 99 {
		t.Errorf("HumanVisitsByDay[4] = %d, want 99", got)
	}
	if r := loaded.ReferrerStats["example.com"]; r == nil || r.Visits[3] != 5 {
		t.Errorf("ReferrerStats not preserved: %+v", r)
	}
}
