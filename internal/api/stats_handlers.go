package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/GenshIv/makoshop/internal/httpres"
)

func (h *Handlers) HandleStatsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	summary := h.statsCollector.GetSummary()
	httpres.WriteJSON(w, http.StatusOK, summary)
}

func (h *Handlers) HandleStatsReferrers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	referrers := h.statsCollector.GetReferrers()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"referrers": referrers,
	})
}

func (h *Handlers) HandleStatsPaths(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	paths := h.statsCollector.GetPaths()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"paths": paths,
	})
}

func (h *Handlers) HandleStatsToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}

	h.statsCollector.SetEnabled(req.Enabled)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": req.Enabled,
	})
}

func (h *Handlers) HandleStatsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": h.statsCollector.IsEnabled(),
	})
}

func (h *Handlers) HandleStatsUserAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	agents := h.statsCollector.GetUserAgents()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"useragents": agents,
	})
}

func (h *Handlers) HandleStatsUpdateExcludedIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		ExcludedIPs []string `json:"excluded_ips"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}

	h.statsCollector.SetExcludedIPs(req.ExcludedIPs)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"excluded_ips": req.ExcludedIPs,
	})
}

func (h *Handlers) HandleStatsGetExcludedIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"excluded_ips": h.statsCollector.GetExcludedIPs(),
	})
}

// Detailed event analytics endpoints

func (h *Handlers) HandleStatsEventsByPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aggregated := h.statsCollector.AggregateByPage()

	// Convert map to sortable slice
	type PageEntry struct {
		Page  string `json:"page"`
		Count uint64 `json:"count"`
	}
	var entries []PageEntry
	for page, count := range aggregated {
		entries = append(entries, PageEntry{Page: page, Count: count})
	}

	// Sort by count descending by default
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"by_page": entries,
		"total":   len(entries),
	})
}

func (h *Handlers) HandleStatsEventsByReferer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aggregated := h.statsCollector.AggregateByReferer()

	// Convert map to sortable slice
	type RefererEntry struct {
		Referer string `json:"referer"`
		Count   uint64 `json:"count"`
	}
	var entries []RefererEntry
	for referer, count := range aggregated {
		entries = append(entries, RefererEntry{Referer: referer, Count: count})
	}

	// Sort by count descending by default
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"by_referer": entries,
		"total":      len(entries),
	})
}

func (h *Handlers) HandleStatsEventsByUA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aggregated := h.statsCollector.AggregateByUA()

	// Build response with resolved UA strings
	type UAResult struct {
		UAID   uint32 `json:"ua_id"`
		UA     string `json:"ua"`
		Visits uint64 `json:"visits"`
	}
	var results []UAResult
	for uaID, visits := range aggregated {
		uaStr := h.statsCollector.GetUAByID(uaID)
		results = append(results, UAResult{UAID: uaID, UA: uaStr, Visits: visits})
	}

	// Sort by visits descending by default
	sort.Slice(results, func(i, j int) bool {
		return results[i].Visits > results[j].Visits
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"by_ua": results,
		"total": len(results),
	})
}

func (h *Handlers) HandleStatsEventsByIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aggregated := h.statsCollector.AggregateByIP()

	// Build response with resolved IP strings
	type IPResult struct {
		IPID   uint32 `json:"ip_id"`
		IP     string `json:"ip"`
		Visits uint64 `json:"visits"`
	}
	var results []IPResult
	for ipID, visits := range aggregated {
		ipStr := h.statsCollector.GetIPByID(ipID)
		results = append(results, IPResult{IPID: ipID, IP: ipStr, Visits: visits})
	}

	// Sort by visits descending by default
	sort.Slice(results, func(i, j int) bool {
		return results[i].Visits > results[j].Visits
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"by_ip": results,
		"total": len(results),
	})
}

func (h *Handlers) HandleStatsEventsByTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aggregated := h.statsCollector.AggregateByTime()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"by_hour": aggregated,
	})
}

func (h *Handlers) HandleStatsBotCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	botCounts := h.statsCollector.GetBotCounts()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"bots": botCounts,
	})
}

func (h *Handlers) HandleStatsEventCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	count := h.statsCollector.EventCount()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"event_count": count,
	})
}
