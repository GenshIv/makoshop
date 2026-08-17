package metrics

// Stats — заглушка для совместимости.
type Stats struct {
	TotalRequests int64            `json:"total_requests"`
	ByCode        map[string]int64 `json:"by_code"`
	AvgNs         int64            `json:"avg_ns"`
	P50Ns         int64            `json:"p50_ns"`
	P95Ns         int64            `json:"p95_ns"`
	P99Ns         int64            `json:"p99_ns"`
}

// ParseMetricsStats — заглушка.
func ParseMetricsStats(dir string) (*Stats, error) {
	return &Stats{ByCode: make(map[string]int64)}, nil
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(p) / 100.0 * float64(len(sorted)-1))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
