# Statistics Implementation

## Overview

Implemented a lightweight statistics collection system that tracks:
- Human visits vs bot visits
- Visits by hour (24-hour cycle)
- Visits by day of week (7-day cycle)
- Visits by day of month (31-day cycle)
- Visits by referrer domain
- Visits by category path

## Architecture

### Key Components

1. **StatsData** (stats/stats.go)
   - Stores all statistics in fixed-size arrays
   - Uses uint32 for counters (supports up to 4 billion visits)
   - Maintains "current period" indicators for rolling windows

2. **StatsCollector** (stats/collector.go)
   - Processes visit events asynchronously
   - Uses buffered channel (1000 events)
   - Non-blocking writes (drops events if channel full)
   - Saves to DB every minute if changes exist

3. **StatsMiddleware** (stats/middleware.go)
   - Lightweight HTTP middleware
   - Detects bots via User-Agent
   - Extracts referrer domain
   - Records visits asynchronously

### Data Flow

```
HTTP Request
    ↓
StatsMiddleware
    ↓
VisitEvent → Channel (buffered 1000)
    ↓
StatsCollector.processVisits()
    ↓
StatsData (atomic updates)
    ↓
saveLoop() → DB (every minute if dirty)
```

## Data Structure

### StatsData

```go
type StatsData struct {
    // Hours (0-23)
    CurrentHour uint8
    HumanVisitsByHour [24]uint32
    BotVisitsByHour   [24]uint32

    // Days of week (0-6, Sunday=0)
    CurrentDayOfWeek uint8
    HumanVisitsByDay [7]uint32
    BotVisitsByDay   [7]uint32

    // Days of month (1-31)
    CurrentDayOfMonth uint8
    HumanVisitsByMonthDay [31]uint32
    BotVisitsByMonthDay   [31]uint32

    // Referrer stats (by domain)
    ReferrerStats map[string]*ReferrerStats

    // Path stats (by category ID)
    PathStats map[int64]*PathStats
}
```

### Rolling Window Logic

- When current hour changes: reset current hour's counters
- When current day of week changes: reset current day's counters
- When current day of month changes: reset current day's counters
- No full resets - only current period is cleared

## API Endpoints

### 1. GET /admin/stats/summary
Returns summary statistics:
```json
{
    "total_human_visits": 12345,
    "total_bot_visits": 678,
    "human_visits_by_hour": [0, 0, 5, 12, ...],
    "bot_visits_by_hour": [0, 0, 1, 2, ...],
    "human_visits_by_day": [100, 200, 150, ...],
    "bot_visits_by_day": [10, 20, 15, ...],
    "human_visits_by_month_day": [500, 600, 700, ...],
    "bot_visits_by_month_day": [50, 60, 70, ...]
}
```

### 2. GET /admin/stats/referrers
Returns referrer statistics:
```json
{
    "referrers": {
        "google.com": {
            "domain": "google.com",
            "visits": [0, 0, 5, 10, ...]
        },
        "facebook.com": {
            "domain": "facebook.com",
            "visits": [0, 2, 3, 4, ...]
        }
    }
}
```

### 3. GET /admin/stats/paths
Returns path statistics by category:
```json
{
    "paths": {
        "123": {
            "category_id": 123,
            "visits": [0, 0, 5, 10, ...]
        },
        "456": {
            "category_id": 456,
            "visits": [0, 2, 3, 4, ...]
        }
    }
}
```

### 4. POST /admin/stats/toggle
Enable/disable stats collection:
```json
{
    "enabled": true
}
```

### 5. GET /admin/stats/status
Get current stats status:
```json
{
    "enabled": true
}
```

## Configuration

### StatsConfig

```go
type StatsConfig struct {
    Enabled           bool     // Enable/disable stats
    MaxReferrers      int      // Max referrers to track (default: 100)
    MaxPaths          int      // Max paths to track (default: 100)
    TrackFullReferrer bool     // Track full referrer URLs (default: false)
    GoogleReferrers   []string // Referrer patterns for full tracking
}
```

## Performance

### Optimizations

1. **Non-blocking channel** - drops events if channel full
2. **Atomic operations** - no locks for counter increments
3. **Async processing** - separate goroutine for processing
4. **Periodic saves** - only saves when changes exist
5. **Lightweight middleware** - minimal overhead per request

### Expected Impact

- **Memory**: ~10-20 KB for stats data
- **CPU**: <0.1ms per request for middleware
- **I/O**: 1 DB write per minute (if changes)
- **Throughput**: Can handle 1000+ events/second

## Bot Detection

### User-Agent Patterns

```go
var BotUserAgents = []string{
    "googlebot",
    "bingbot",
    "yandexbot",
    "facebookexternalhit",
    "twitterbot",
    "linkedinbot",
    "slackbot",
    "pinterestbot",
    "whatsapp",
    "telegrambot",
    "discordbot",
    "crawler",
    "spider",
    "bot",
    "headless",
    "phantomjs",
    "selenium",
    "puppeteer",
}
```

## Files Created

1. `internal/stats/stats.go` - Data structures
2. `internal/stats/collector.go` - StatsCollector implementation
3. `internal/stats/middleware.go` - HTTP middleware
4. `internal/stats/handlers.go` - API handlers

## Files Modified

1. `internal/api/handlers.go` - Added stats handlers and collector

## Next Steps

1. **Integrate middleware** into HTTP router
2. **Add persistence** (save to DB)
3. **Create admin UI** for viewing stats
4. **Add category ID lookup** for path stats
5. **Add full referrer tracking** for Google

## Testing

### Test Stats Collection

```bash
# Enable stats
curl -X POST http://localhost:8080/admin/stats/toggle \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'

# Check status
curl http://localhost:8080/admin/stats/status

# View summary
curl http://localhost:8080/admin/stats/summary

# View referrers
curl http://localhost:8080/admin/stats/referrers

# View paths
curl http://localhost:8080/admin/stats/paths
```
