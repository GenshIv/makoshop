package api

import (
	"encoding/json"
	"net/http"

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
