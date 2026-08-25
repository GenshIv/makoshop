package stats

import (
	"encoding/json"
	"net/http"

	"github.com/GenshIv/makoshop/internal/httpres"
)

// StatsHandler handles stats API requests
type StatsHandler struct {
	collector *StatsCollector
}

// NewStatsHandler creates a new StatsHandler
func NewStatsHandler(collector *StatsCollector) *StatsHandler {
	return &StatsHandler{
		collector: collector,
	}
}

// HandleGetSummary handles GET /admin/stats/summary
func (h *StatsHandler) HandleGetSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteErrorFlat(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	summary := h.collector.GetSummary()
	httpres.WriteJSON(w, http.StatusOK, summary)
}

// HandleGetReferrers handles GET /admin/stats/referrers
func (h *StatsHandler) HandleGetReferrers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteErrorFlat(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	referrers := h.collector.GetReferrers()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"referrers": referrers,
	})
}

// HandleGetPaths handles GET /admin/stats/paths
func (h *StatsHandler) HandleGetPaths(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteErrorFlat(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	paths := h.collector.GetPaths()
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"paths": paths,
	})
}

// HandleToggleStats handles POST /admin/stats/toggle
func (h *StatsHandler) HandleToggleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteErrorFlat(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpres.WriteErrorFlat(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}

	h.collector.SetEnabled(req.Enabled)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": req.Enabled,
	})
}

// HandleGetStatus handles GET /admin/stats/status
func (h *StatsHandler) HandleGetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteErrorFlat(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": h.collector.IsEnabled(),
	})
}
