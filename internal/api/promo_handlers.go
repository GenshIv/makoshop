package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

func (h *Handlers) HandlePromoPlansList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	plans, err := h.promoPlanRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if plans == nil {
		plans = []model.PromoPlan{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": plans,
		"total": len(plans),
	})
}

// HandleAdminPromoPlanCreate creates a promo plan (admin only).
// POST /admin/promo-plans

func (h *Handlers) HandleAdminPromoPlanCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	var req struct {
		Name         string           `json:"name"`
		Type         string           `json:"type"`
		DurationDays int              `json:"duration_days"`
		Price        float64          `json:"price"`
		Currency     string           `json:"currency"`
		Description  string           `json:"description,omitempty"`
		Constraints  []model.KeyValue `json:"constraints,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Type == "" || req.DurationDays <= 0 || req.Price <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "name, type, duration_days > 0, price > 0 required")
		return
	}

	p := &model.PromoPlan{
		Name:         req.Name,
		Type:         model.PromoPlanType(req.Type),
		DurationDays: req.DurationDays,
		Price:        req.Price,
		Currency:     req.Currency,
		Description:  req.Description,
		Constraints:  req.Constraints,
	}
	if p.Currency == "" {
		p.Currency = "RUB"
	}

	if err := h.promoPlanRepo.Create(p); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, p)
}

// HandleAdminPromoPlanUpdate updates a promo plan (admin only).
// PATCH /admin/promo-plans/{id}

func (h *Handlers) HandleAdminPromoPlanUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	id, ok := parseID(w, r, "promo_plan_id")
	if !ok {
		return
	}

	var req struct {
		Name         string           `json:"name,omitempty"`
		Type         string           `json:"type,omitempty"`
		DurationDays int              `json:"duration_days"`
		Price        float64          `json:"price"`
		Currency     string           `json:"currency,omitempty"`
		Description  string           `json:"description,omitempty"`
		Constraints  []model.KeyValue `json:"constraints,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.promoPlanRepo.Update(id, func(p *model.PromoPlan) {
		if req.Name != "" {
			p.Name = req.Name
		}
		if req.Type != "" {
			p.Type = model.PromoPlanType(req.Type)
		}
		if req.DurationDays > 0 {
			p.DurationDays = req.DurationDays
		}
		if req.Price > 0 {
			p.Price = req.Price
		}
		if req.Currency != "" {
			p.Currency = req.Currency
		}
		if req.Description != "" {
			p.Description = req.Description
		}
		if req.Constraints != nil {
			p.Constraints = req.Constraints
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	p, _ := h.promoPlanRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, p)
}

// HandleAdminPromoCampaignCreate creates a campaign (admin only).
// POST /admin/promo/campaigns

func (h *Handlers) HandleAdminPromoCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	var req struct {
		CompanyID      int64               `json:"company_id"`
		PromoPlanID    int64               `json:"promo_plan_id"`
		Status         string              `json:"status"`
		TargetFilters  model.TargetFilters `json:"target_filters"`
		TargetPosition string              `json:"target_position"`
		ProductIDs     []int64             `json:"product_ids,omitempty"`
		BudgetTotal    float64             `json:"budget_total"`
		StartAt        string              `json:"start_at,omitempty"`
		EndAt          string              `json:"end_at,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.CompanyID <= 0 || req.PromoPlanID <= 0 || req.BudgetTotal <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id > 0, promo_plan_id > 0, budget_total > 0 required")
		return
	}

	plan, err := h.promoPlanRepo.Get(req.PromoPlanID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "promo_plan not found")
		return
	}

	startAt := time.Now().Unix()
	endAt := startAt + int64(plan.DurationDays)*86400
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t.Unix()
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t.Unix()
		}
	}

	status := model.PromoCampaignStatusActive
	if req.Status != "" {
		status = model.PromoCampaignStatus(req.Status)
	}

	c := &model.PromoCampaign{
		CompanyID:      req.CompanyID,
		PromoPlanID:    req.PromoPlanID,
		Status:         status,
		TargetFilters:  req.TargetFilters,
		TargetPosition: req.TargetPosition,
		ProductIDs:     req.ProductIDs,
		BudgetTotal:    req.BudgetTotal,
		BudgetUsed:     0,
		StartAt:        startAt,
		EndAt:          endAt,
	}

	if err := h.promoCampaignRepo.Create(c); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, c)
}

// HandleAdminPromoCampaignsList returns all campaigns (admin only).
// GET /admin/promo/campaigns?status=...

func (h *Handlers) HandleAdminPromoCampaignsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	campaigns, err := h.promoCampaignRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if campaigns == nil {
		campaigns = []model.PromoCampaign{}
	}

	// Filter by status if provided
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	if statusFilter != "" {
		var filtered []model.PromoCampaign
		for _, c := range campaigns {
			if string(c.Status) == statusFilter {
				filtered = append(filtered, c)
			}
		}
		campaigns = filtered
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": campaigns,
		"total": len(campaigns),
	})
}

// HandleAdminPromoCampaignUpdate updates a campaign (admin only).
// PATCH /admin/promo/campaigns/{id}

func (h *Handlers) HandleAdminPromoCampaignUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	id, ok := parseID(w, r, "promo_campaign_id")
	if !ok {
		return
	}

	c, err := h.promoCampaignRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}

	var req struct {
		Status         string               `json:"status,omitempty"`
		TargetFilters  *model.TargetFilters `json:"target_filters,omitempty"`
		TargetPosition string               `json:"target_position,omitempty"`
		BudgetTotal    float64              `json:"budget_total"`
		StartAt        string               `json:"start_at,omitempty"`
		EndAt          string               `json:"end_at,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.promoCampaignRepo.Update(id, func(camp *model.PromoCampaign) {
		if req.Status != "" {
			camp.Status = model.PromoCampaignStatus(req.Status)
		}
		if req.TargetFilters != nil {
			camp.TargetFilters = *req.TargetFilters
		}
		if req.TargetPosition != "" {
			camp.TargetPosition = req.TargetPosition
		}
		if req.BudgetTotal > 0 {
			camp.BudgetTotal = req.BudgetTotal
		}
		if req.StartAt != "" {
			if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
				camp.StartAt = t.Unix()
			}
		}
		if req.EndAt != "" {
			if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
				camp.EndAt = t.Unix()
			}
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ = h.promoCampaignRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandleCompanyPromoCampaignsList returns campaigns for a company.
// GET /companies/{companyId}/promo-campaigns
// Access: seller (own) or admin.

func (h *Handlers) HandleCompanyPromoCampaignsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /companies/{id}/promo-campaigns
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "promo-campaigns" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	// Access control
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	if ctxUser.Role != model.RoleAdmin {
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || companyID != ownerCompanyID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only view campaigns for your own company")
			return
		}
	}

	campaigns, err := h.promoCampaignRepo.ListByCompany(companyID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if campaigns == nil {
		campaigns = []model.PromoCampaign{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": companyID,
		"items":      campaigns,
		"total":      len(campaigns),
	})
}

// HandleCompanyPromoCampaignCreate creates a promo campaign (seller only, own company).
// POST /companies/{companyId}/promo-campaigns

func (h *Handlers) HandleCompanyPromoCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /companies/{id}/promo-campaigns
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "promo-campaigns" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleSeller {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "seller access required")
		return
	}
	ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
	if err != nil || companyID != ownerCompanyID {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only create campaigns for your own company")
		return
	}

	var req struct {
		PromoPlanID    int64               `json:"promo_plan_id"`
		TargetFilters  model.TargetFilters `json:"target_filters"`
		TargetPosition string              `json:"target_position"`
		BudgetTotal    float64             `json:"budget_total"`
		StartAt        string              `json:"start_at,omitempty"`
		EndAt          string              `json:"end_at,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.PromoPlanID <= 0 || req.BudgetTotal <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "promo_plan_id > 0 and budget_total > 0 required")
		return
	}

	// Verify plan exists
	plan, err := h.promoPlanRepo.Get(req.PromoPlanID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "promo_plan not found")
		return
	}

	startAt := time.Now().Unix()
	endAt := startAt + int64(plan.DurationDays)*86400
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t.Unix()
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t.Unix()
		}
	}

	c := &model.PromoCampaign{
		CompanyID:      companyID,
		PromoPlanID:    req.PromoPlanID,
		Status:         model.PromoCampaignStatusPending,
		TargetFilters:  req.TargetFilters,
		TargetPosition: req.TargetPosition,
		BudgetTotal:    req.BudgetTotal,
		BudgetUsed:     0,
		StartAt:        startAt,
		EndAt:          endAt,
	}

	if err := h.promoCampaignRepo.Create(c); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, c)
}

// HandlePromoCampaignUpdateStatus updates campaign status.
// PATCH /promo-campaigns/{id}/status
// Access: seller (own company) or admin.

func (h *Handlers) HandlePromoCampaignUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /promo-campaigns/{id}/status -> parts = ["", "promo-campaigns", "{id}", "status"]
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[1] != "promo-campaigns" || parts[3] != "status" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid campaign id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.Status == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "status is required")
		return
	}

	c, err := h.promoCampaignRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	if ctxUser.Role != model.RoleAdmin {
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || c.CompanyID != ownerCompanyID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage campaigns for your own company")
			return
		}
	}

	if err := h.promoCampaignRepo.UpdateStatus(id, model.PromoCampaignStatus(req.Status)); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ = h.promoCampaignRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandlePromoLogCreate records a promo event.
// POST /promo/logs
// Body: {"campaign_id": 1, "event_type": "impression", "context": {...}, "cost": 0.01}

func (h *Handlers) HandlePromoLogCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		CampaignID int64            `json:"campaign_id"`
		EventType  string           `json:"event_type"`
		Context    []model.KeyValue `json:"context,omitempty"`
		Cost       float64          `json:"cost"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.CampaignID <= 0 || req.EventType == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "campaign_id > 0 and event_type required")
		return
	}

	// Verify campaign exists and is active
	c, err := h.promoCampaignRepo.Get(req.CampaignID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}
	if c.Status != model.PromoCampaignStatusActive {
		httpres.WriteError(w, http.StatusBadRequest, "INVALID_STATE", "campaign must be active")
		return
	}

	// Update budget_used
	if req.Cost > 0 {
		_ = h.promoCampaignRepo.Update(req.CampaignID, func(camp *model.PromoCampaign) {
			camp.BudgetUsed += req.Cost
		})
	}

	l := &model.PromoLog{
		CampaignID: req.CampaignID,
		EventType:  model.PromoEventType(req.EventType),
		Context:    req.Context,
		Cost:       req.Cost,
	}

	if err := h.promoLogRepo.Create(l); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, l)
}

// ================= Product Import handlers =================

// HandleAdminProductsReindex rebuilds all product indexes.
// POST /admin/products/reindex
