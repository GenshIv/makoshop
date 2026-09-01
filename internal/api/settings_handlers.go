package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

func (h *Handlers) HandlePaymentMethodsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.paymentMethodRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.CompanyPaymentMethod{}
	}
	httpres.WriteJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandlePaymentMethodCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var pm model.CompanyPaymentMethod
	if !httpres.ReadJSON(w, r, &pm) {
		return
	}
	if err := h.paymentMethodRepo.Create(&pm); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusCreated, pm)
}

func (h *Handlers) HandlePaymentMethodGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	pm, err := h.paymentMethodRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, pm)
}

func (h *Handlers) HandlePaymentMethodUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	var pm model.CompanyPaymentMethod
	if !httpres.ReadJSON(w, r, &pm) {
		return
	}
	if err := h.paymentMethodRepo.Update(id, func(p *model.CompanyPaymentMethod) {
		if pm.Name != "" {
			p.Name = pm.Name
		}
		if pm.Slug != "" {
			p.Slug = pm.Slug
		}
		p.IsActive = pm.IsActive
		if pm.SortOrder != 0 {
			p.SortOrder = pm.SortOrder
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.paymentMethodRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandlePaymentMethodDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	if err := h.paymentMethodRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Delivery Times CRUD ---

func (h *Handlers) HandleDeliveryTimesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.deliveryTimeRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.DeliveryTime{}
	}
	httpres.WriteJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandleDeliveryTimeCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var dt model.DeliveryTime
	if !httpres.ReadJSON(w, r, &dt) {
		return
	}
	if err := h.deliveryTimeRepo.Create(&dt); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusCreated, dt)
}

func (h *Handlers) HandleDeliveryTimeGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	dt, err := h.deliveryTimeRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, dt)
}

func (h *Handlers) HandleDeliveryTimeUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	var dt model.DeliveryTime
	if !httpres.ReadJSON(w, r, &dt) {
		return
	}
	if err := h.deliveryTimeRepo.Update(id, func(d *model.DeliveryTime) {
		if dt.Name != "" {
			d.Name = dt.Name
		}
		if dt.Slug != "" {
			d.Slug = dt.Slug
		}
		d.IsActive = dt.IsActive
		if dt.SortOrder != 0 {
			d.SortOrder = dt.SortOrder
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.deliveryTimeRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandleDeliveryTimeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	if err := h.deliveryTimeRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Delivery Methods CRUD ---

func (h *Handlers) HandleDeliveryMethodsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.deliveryMethodRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.DeliveryMethod{}
	}
	httpres.WriteJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandleDeliveryMethodCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var dm model.DeliveryMethod
	if !httpres.ReadJSON(w, r, &dm) {
		return
	}
	if dm.Slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "slug is required")
		return
	}
	if err := h.deliveryMethodRepo.Create(&dm); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusCreated, dm)
}

func (h *Handlers) HandleDeliveryMethodGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_method_id")
	if !ok {
		return
	}
	dm, err := h.deliveryMethodRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, dm)
}

func (h *Handlers) HandleDeliveryMethodUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_method_id")
	if !ok {
		return
	}
	var req struct {
		Name      string  `json:"name"`
		Slug      string  `json:"slug"`
		Image     *string `json:"image"`
		IsActive  bool    `json:"is_active"`
		SortOrder int     `json:"sort_order"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.Slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "slug is required")
		return
	}
	if err := h.deliveryMethodRepo.Update(id, func(d *model.DeliveryMethod) {
		if req.Name != "" {
			d.Name = req.Name
		}
		if req.Slug != "" {
			d.Slug = req.Slug
		}
		if req.Image != nil {
			d.Image = *req.Image
		}
		d.IsActive = req.IsActive
		if req.SortOrder != 0 {
			d.SortOrder = req.SortOrder
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.deliveryMethodRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandleDeliveryMethodDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_method_id")
	if !ok {
		return
	}
	if err := h.deliveryMethodRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Installment Plans CRUD ---

func (h *Handlers) HandleInstallmentPlansList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.installmentPlanRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.InstallmentPlan{}
	}
	httpres.WriteJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandleInstallmentPlanCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var ip model.InstallmentPlan
	if !httpres.ReadJSON(w, r, &ip) {
		return
	}
	if err := h.installmentPlanRepo.Create(&ip); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusCreated, ip)
}

func (h *Handlers) HandleInstallmentPlanGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	ip, err := h.installmentPlanRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, ip)
}

func (h *Handlers) HandleInstallmentPlanUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	var ip model.InstallmentPlan
	if !httpres.ReadJSON(w, r, &ip) {
		return
	}
	if err := h.installmentPlanRepo.Update(id, func(i *model.InstallmentPlan) {
		if ip.Name != "" {
			i.Name = ip.Name
		}
		if ip.Slug != "" {
			i.Slug = ip.Slug
		}
		i.IsActive = ip.IsActive
		if ip.SortOrder != 0 {
			i.SortOrder = ip.SortOrder
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.installmentPlanRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandleInstallmentPlanDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	if err := h.installmentPlanRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Company settings: get with full details ---

func (h *Handlers) HandleCompanyGetWithSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /admin/companies/{id}/settings
	path := r.URL.Path
	// Trim trailing "/settings"
	if !strings.HasSuffix(path, "/settings") {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	prefix := strings.TrimSuffix(path, "/settings")
	// prefix is now "/admin/companies/{id}"
	parts := strings.Split(prefix, "/")
	if len(parts) < 4 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing company id")
		return
	}
	idStr := parts[3]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Load settings from separate storage
	settings, _ := h.companyRepo.GetCompanySettings(id)
	if settings != nil {
		c.PaymentMethodIds = settings.PaymentMethodIds
		c.DeliveryTimeIds = settings.DeliveryTimeIds
		c.DeliveryMethodIds = settings.DeliveryMethodIds
		c.InstallmentPlanIds = settings.InstallmentPlanIds
	}

	// Load full objects for payment methods, delivery times, delivery methods, installment plans
	var paymentMethods []model.CompanyPaymentMethod
	var deliveryTimes []model.DeliveryTime
	var deliveryMethods []model.DeliveryMethod
	var installmentPlans []model.InstallmentPlan

	if len(c.PaymentMethodIds) > 0 {
		for _, id := range c.PaymentMethodIds {
			if pm, err := h.paymentMethodRepo.Get(id); err == nil {
				paymentMethods = append(paymentMethods, *pm)
			}
		}
	}
	if len(c.DeliveryTimeIds) > 0 {
		for _, id := range c.DeliveryTimeIds {
			if dt, err := h.deliveryTimeRepo.Get(id); err == nil {
				deliveryTimes = append(deliveryTimes, *dt)
			}
		}
	}
	if len(c.DeliveryMethodIds) > 0 {
		for _, id := range c.DeliveryMethodIds {
			if dm, err := h.deliveryMethodRepo.Get(id); err == nil {
				deliveryMethods = append(deliveryMethods, *dm)
			}
		}
	}
	if len(c.InstallmentPlanIds) > 0 {
		for _, id := range c.InstallmentPlanIds {
			if ip, err := h.installmentPlanRepo.Get(id); err == nil {
				installmentPlans = append(installmentPlans, *ip)
			}
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"company":           c,
		"payment_methods":   paymentMethods,
		"delivery_times":    deliveryTimes,
		"delivery_methods":  deliveryMethods,
		"installment_plans": installmentPlans,
	})
}

// --- Home page hero text (manual management) ---

// homeHeroLangs are the locales with a hero text override on the main page.
var homeHeroLangs = []string{"ru", "ua", "en", "pl"}

// homeHeroFields are the hero block fields that can be overridden per locale.
var homeHeroFields = []string{"headline", "sub", "tagline"}

// homeHeroFieldMaxLen caps a single hero text field (defense against garbage).
const homeHeroFieldMaxLen = 300

// normalizeHomeHero validates and normalizes the home hero text payload
// (per-locale overrides for the main page hero block). Unknown locales and
// fields are dropped; an empty string means "use the default i18n text".
func normalizeHomeHero(raw map[string]interface{}) (map[string]map[string]string, error) {
	result := map[string]map[string]string{}
	for _, lang := range homeHeroLangs {
		block, ok := raw[lang].(map[string]interface{})
		if !ok {
			continue
		}
		texts := map[string]string{}
		for _, field := range homeHeroFields {
			v, _ := block[field].(string)
			v = strings.TrimSpace(v)
			if len(v) > homeHeroFieldMaxLen {
				return nil, fmt.Errorf("home hero %s.%s: max %d characters", lang, field, homeHeroFieldMaxLen)
			}
			texts[field] = v
		}
		result[lang] = texts
	}
	return result, nil
}

// HandleGlobalSettingsGet returns global settings including default currency.
// GET /admin/settings
func (h *Handlers) HandleGlobalSettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	defaultCurrency := "PLN" // default currency
	gaMeasurementID := ""    // Google Analytics measurement ID (empty = GA disabled)
	homeHero := map[string]map[string]string{}
	if h.Store != nil {
		store := h.Store()
		if val, err := store.DocGet("global_settings"); err == nil && len(val) > 0 {
			var settings map[string]interface{}
			if err := json.Unmarshal(val, &settings); err == nil {
				if cur, ok := settings["default_currency"].(string); ok && cur != "" {
					defaultCurrency = cur
				}
				if ga, ok := settings["ga_measurement_id"].(string); ok {
					gaMeasurementID = strings.TrimSpace(ga)
				}
				if raw, ok := settings["home_hero"].(map[string]interface{}); ok {
					if hh, err := normalizeHomeHero(raw); err == nil {
						homeHero = hh
					}
				}
			}
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"default_currency":  defaultCurrency,
		"ga_measurement_id": gaMeasurementID,
		"home_hero":         homeHero,
	})
}

// HandleGlobalSettingsUpdate updates global settings including default currency.
// PATCH /admin/settings
func (h *Handlers) HandleGlobalSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		DefaultCurrency string                 `json:"default_currency,omitempty"`
		GaMeasurementID string                 `json:"ga_measurement_id"`
		HomeHero        map[string]interface{} `json:"home_hero"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if h.Store == nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "store not available")
		return
	}

	store := h.Store()

	// Load existing settings or create new
	settings := map[string]interface{}{}
	if val, err := store.DocGet("global_settings"); err == nil && len(val) > 0 {
		json.Unmarshal(val, &settings)
	}

	// Update currency if provided
	if req.DefaultCurrency != "" {
		settings["default_currency"] = req.DefaultCurrency
	}

	// GA measurement ID is always written (empty string disables tracking) so
	// admins can both set and clear it from the settings UI.
	settings["ga_measurement_id"] = strings.TrimSpace(req.GaMeasurementID)

	// Home hero text (manual management of the main page hero block).
	// When provided, the whole per-locale payload is replaced.
	if req.HomeHero != nil {
		hh, err := normalizeHomeHero(req.HomeHero)
		if err != nil {
			httpres.WriteError(w, http.StatusBadRequest, "INVALID_PARAM", err.Error())
			return
		}
		settings["home_hero"] = hh
	}

	// Save
	data, err := json.Marshal(settings)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if err := store.DocPut("global_settings", data); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"default_currency":  settings["default_currency"],
		"ga_measurement_id": settings["ga_measurement_id"],
		"home_hero":         settings["home_hero"],
	})
}
