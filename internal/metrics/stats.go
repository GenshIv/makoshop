package metrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Stats — агрегированная статистика из метрик.
type Stats struct {
	TotalRequests int64            `json:"total_requests"`
	ByCode        map[string]int64 `json:"by_code"`
	AvgNs         int64            `json:"avg_ns"`
	P50Ns         int64            `json:"p50_ns"`
	P95Ns         int64            `json:"p95_ns"`
	P99Ns         int64            `json:"p99_ns"`
	TopURLs       []URLStat        `json:"top_urls"`    // по частоте
	SlowURLs      []URLStat        `json:"slow_urls"`   // по среднему времени
	TopRoutes     []RouteStat      `json:"top_routes"`  // по частоте
	SlowRoutes    []RouteStat      `json:"slow_routes"` // по среднему времени

	// Time series: bucket’ы по 1 минуте
	RPSOverTime     []TimeBucket `json:"rps_over_time"`
	LatencyOverTime []TimeBucket `json:"latency_over_time"` // avg latency in ms
}

// TimeBucket — один bucket по времени.
type TimeBucket struct {
	TimestampMs int64   `json:"ts"`
	Count       int64   `json:"count"`
	AvgMs       float64 `json:"avg_ms"`
}

type URLStat struct {
	URL   string `json:"url"`
	Count int64  `json:"count"`
	AvgNs int64  `json:"avg_ns"`
	P95Ns int64  `json:"p95_ns"`
	MaxNs int64  `json:"max_ns"`
}

type RouteStat struct {
	Route string `json:"route"`
	Count int64  `json:"count"`
	AvgNs int64  `json:"avg_ns"`
	P95Ns int64  `json:"p95_ns"`
	MaxNs int64  `json:"max_ns"`
}

// ParseMetricsStats читает последние N строк из метрик и возвращает агрегаты.
func ParseMetricsStats(dir string) (*Stats, error) {
	const maxLines = 200000 // берём только последние 200K строк

	files, err := filepath.Glob(filepath.Join(dir, "metrics-*.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &Stats{
			ByCode:     make(map[string]int64),
			TopURLs:    []URLStat{},
			SlowURLs:   []URLStat{},
			TopRoutes:  []RouteStat{},
			SlowRoutes: []RouteStat{},
		}, nil
	}

	// Берём последний файл по имени
	sort.Strings(files)
	lastFile := files[len(files)-1]

	// Используем tail для чтения только последних N строк
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", maxLines), lastFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	type urlData struct {
		count   int64
		sumNs   int64
		maxNs   int64
		samples []int64
	}
	type routeData struct {
		count   int64
		sumNs   int64
		maxNs   int64
		samples []int64
	}

	var total int64
	var sumNs int64
	var allSamples []int64
	byCode := make(map[string]int64)
	urls := make(map[string]*urlData)
	routes := make(map[string]*routeData)

	// Buckets по 1 минуте (60000 ms)
	const bucketSizeMs = 60000
	buckets := make(map[int64]*struct {
		count int64
		sumNs int64
	})

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}

		total++
		sumNs += e.DurationNs
		byCode[fmt.Sprintf("%d", e.Code)]++

		allSamples = append(allSamples, e.DurationNs)

		// Bucket по времени
		bucketKey := (e.TimestampMs / bucketSizeMs) * bucketSizeMs
		b := buckets[bucketKey]
		if b == nil {
			b = &struct {
				count int64
				sumNs int64
			}{}
			buckets[bucketKey] = b
		}
		b.count++
		b.sumNs += e.DurationNs

		// URL stats
		ud := urls[e.URL]
		if ud == nil {
			ud = &urlData{}
			urls[e.URL] = ud
		}
		ud.count++
		ud.sumNs += e.DurationNs
		if e.DurationNs > ud.maxNs {
			ud.maxNs = e.DurationNs
		}
		if ud.count <= 100 {
			ud.samples = append(ud.samples, e.DurationNs)
		}

		// Route stats (simple normalization)
		route := normalizeRoute(e.URL)
		rd := routes[route]
		if rd == nil {
			rd = &routeData{}
			routes[route] = rd
		}
		rd.count++
		rd.sumNs += e.DurationNs
		if e.DurationNs > rd.maxNs {
			rd.maxNs = e.DurationNs
		}
		if rd.count <= 100 {
			rd.samples = append(rd.samples, e.DurationNs)
		}
	}

	_ = cmd.Wait()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Global percentiles
	var avgNs int64
	if total > 0 {
		avgNs = sumNs / total
	}
	sort.Slice(allSamples, func(i, j int) bool {
		return allSamples[i] < allSamples[j]
	})

	stats := &Stats{
		TotalRequests: total,
		ByCode:        byCode,
		AvgNs:         avgNs,
		P50Ns:         percentile(allSamples, 50),
		P95Ns:         percentile(allSamples, 95),
		P99Ns:         percentile(allSamples, 99),
	}

	// Build time buckets
	var bucketKeys []int64
	for k := range buckets {
		bucketKeys = append(bucketKeys, k)
	}
	sort.Slice(bucketKeys, func(i, j int) bool {
		return bucketKeys[i] < bucketKeys[j]
	})

	for _, k := range bucketKeys {
		b := buckets[k]
		avgMs := 0.0
		if b.count > 0 {
			avgMs = float64(b.sumNs) / float64(b.count) / 1e6
		}
		tb := TimeBucket{
			TimestampMs: k,
			Count:       b.count,
			AvgMs:       avgMs,
		}
		stats.RPSOverTime = append(stats.RPSOverTime, tb)
		stats.LatencyOverTime = append(stats.LatencyOverTime, tb)
	}

	// Top URLs by count
	type urlEntry struct {
		url string
		ud  *urlData
	}
	var urlEntries []urlEntry
	for u, ud := range urls {
		urlEntries = append(urlEntries, urlEntry{url: u, ud: ud})
	}
	sort.Slice(urlEntries, func(i, j int) bool {
		return urlEntries[i].ud.count > urlEntries[j].ud.count
	})
	for i, e := range urlEntries {
		if i >= 10 {
			break
		}
		ud := e.ud
		stats.TopURLs = append(stats.TopURLs, URLStat{
			URL:   e.url,
			Count: ud.count,
			AvgNs: ud.sumNs / ud.count,
			P95Ns: percentile(ud.samples, 95),
			MaxNs: ud.maxNs,
		})
	}

	// Slow URLs by avg
	sort.Slice(urlEntries, func(i, j int) bool {
		ai := urlEntries[i].ud.sumNs / urlEntries[i].ud.count
		aj := urlEntries[j].ud.sumNs / urlEntries[j].ud.count
		return ai > aj
	})
	for i, e := range urlEntries {
		if i >= 10 {
			break
		}
		ud := e.ud
		stats.SlowURLs = append(stats.SlowURLs, URLStat{
			URL:   e.url,
			Count: ud.count,
			AvgNs: ud.sumNs / ud.count,
			P95Ns: percentile(ud.samples, 95),
			MaxNs: ud.maxNs,
		})
	}

	// Top routes by count
	type routeEntry struct {
		route string
		rd    *routeData
	}
	var routeEntries []routeEntry
	for r, rd := range routes {
		routeEntries = append(routeEntries, routeEntry{route: r, rd: rd})
	}
	sort.Slice(routeEntries, func(i, j int) bool {
		return routeEntries[i].rd.count > routeEntries[j].rd.count
	})
	for i, e := range routeEntries {
		if i >= 10 {
			break
		}
		rd := e.rd
		stats.TopRoutes = append(stats.TopRoutes, RouteStat{
			Route: e.route,
			Count: rd.count,
			AvgNs: rd.sumNs / rd.count,
			P95Ns: percentile(rd.samples, 95),
			MaxNs: rd.maxNs,
		})
	}

	// Slow routes by avg
	sort.Slice(routeEntries, func(i, j int) bool {
		ai := routeEntries[i].rd.sumNs / routeEntries[i].rd.count
		aj := routeEntries[j].rd.sumNs / routeEntries[j].rd.count
		return ai > aj
	})
	for i, e := range routeEntries {
		if i >= 10 {
			break
		}
		rd := e.rd
		stats.SlowRoutes = append(stats.SlowRoutes, RouteStat{
			Route: e.route,
			Count: rd.count,
			AvgNs: rd.sumNs / rd.count,
			P95Ns: percentile(rd.samples, 95),
			MaxNs: rd.maxNs,
		})
	}

	return stats, nil
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := float64(p) / 100.0 * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower] + int64(float64(sorted[upper]-sorted[lower])*frac)
}

func normalizeRoute(urlStr string) string {
	// Простая нормализация: убираем query string, заменяем числовые сегменты на :id
	if idx := strings.Index(urlStr, "?"); idx >= 0 {
		urlStr = urlStr[:idx]
	}
	if urlStr == "" {
		return "/"
	}
	parts := splitPath(urlStr)
	for i, p := range parts {
		if len(p) > 0 && isNumeric(p) {
			parts[i] = ":id"
		}
	}
	return joinPath(parts)
}

func splitPath(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}
	if path[0] == '/' {
		path = path[1:]
	}
	var res []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if i > start {
				res = append(res, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		res = append(res, path[start:])
	}
	if len(res) == 0 {
		return []string{""}
	}
	return res
}

func joinPath(parts []string) string {
	if len(parts) == 1 && parts[0] == "" {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
