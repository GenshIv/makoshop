package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

func (h *Handlers) HandlePaymentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	orderIDStr := r.URL.Query().Get("order_id")
	if orderIDStr == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "order_id is required")
		return
	}

	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil || orderID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid order_id")
		return
	}

	// Get order to fetch amount
	order, err := h.orderRepo.Get(orderID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	var req struct {
		Method string `json:"method"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	method := model.PaymentMethod(req.Method)
	if method == "" {
		method = model.PaymentMethodCard
	}

	// Stub: generate a fake payment URL
	payment := &model.Payment{
		OrderID:    orderID,
		Amount:     order.TotalAmount,
		Currency:   order.Currency,
		Method:     method,
		Status:     model.PaymentStatusPending,
		PaymentURL: fmt.Sprintf("https://payment.example.com/pay/%d", orderID),
	}

	if err := h.paymentRepo.Create(payment); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, payment)
}

// HandlePaymentConfirm confirms a payment (stub).
// POST /payments/{id}/confirm
// Body: {}

func (h *Handlers) HandlePaymentConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/{id}/confirm
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "{id}", "confirm"]
	if len(parts) < 4 || parts[2] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing payment id")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid payment id")
		return
	}

	// Update payment status
	if err := h.paymentRepo.UpdateStatus(id, model.PaymentStatusPaid); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Also update order payment_status
	payment, _ := h.paymentRepo.Get(id)
	if payment != nil {
		_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
			o.PaymentStatus = model.PaymentStatusPaid
		})
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "confirmed",
	})
}

// HandlePaymentGet returns a payment by ID.
// GET /payments/{id}

func (h *Handlers) HandlePaymentGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "payment_id")
	if !ok {
		return
	}

	payment, err := h.paymentRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, payment)
}

// HandlePaymentWebhook handles payment webhooks from external providers.
// POST /payments/webhook/{provider}
// Body: provider-specific payload (stub: expects {"payment_id": 123, "status": "paid"|"failed"})
// Signature verification: header X-Webhook-Signature (simple HMAC-SHA256 stub).

func (h *Handlers) HandlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/webhook/{provider}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "webhook", "{provider}"]
	if len(parts) < 4 || parts[2] != "webhook" || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid webhook path")
		return
	}
	provider := parts[3]

	// Stub signature verification (in production: verify HMAC)
	signature := r.Header.Get("X-Webhook-Signature")
	if signature == "" {
		httpres.WriteError(w, http.StatusForbidden, "INVALID_SIGNATURE", "missing X-Webhook-Signature")
		return
	}
	// TODO: real HMAC verification per provider
	_ = provider

	var payload struct {
		PaymentID int64  `json:"payment_id"`
		Status    string `json:"status"`
	}
	if !httpres.ReadJSON(w, r, &payload) {
		return
	}

	if payload.PaymentID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "payment_id is required")
		return
	}

	payment, err := h.paymentRepo.Get(payload.PaymentID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	// Idempotency: ignore if already in final state
	if payment.Status == model.PaymentStatusRefunded {
		httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_refunded"})
		return
	}

	var newStatus model.PaymentStatus
	switch strings.ToLower(payload.Status) {
	case "paid", "succeeded", "success":
		newStatus = model.PaymentStatusPaid
	case "failed", "declined", "error":
		newStatus = model.PaymentStatusFailed
	default:
		httpres.WriteError(w, http.StatusBadRequest, "INVALID_STATUS", "unknown payment status from provider")
		return
	}

	// Idempotency: if already in this status, no-op
	if payment.Status == newStatus {
		httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_processed"})
		return
	}

	// Update payment
	if err := h.paymentRepo.UpdateStatus(payload.PaymentID, newStatus); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Update order payment_status
	_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
		o.PaymentStatus = newStatus
		if newStatus == model.PaymentStatusFailed {
			o.Status = model.OrderStatusCancelled
		}
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "payment_status": string(newStatus)})
}

// HandlePaymentRefund refunds a payment.
// POST /payments/{id}/refund
// Body: {} (full refund; partial refund support can be added later)
// Access: admin only.

func (h *Handlers) HandlePaymentRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/{id}/refund
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "{id}", "refund"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "refund" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid payment id")
		return
	}

	// Check auth: admin only
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	payment, err := h.paymentRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	if payment.Status != model.PaymentStatusPaid {
		httpres.WriteError(w, http.StatusBadRequest, "INVALID_STATE", "only paid payments can be refunded")
		return
	}

	// Update payment to refunded
	if err := h.paymentRepo.UpdateStatus(id, model.PaymentStatusRefunded); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Update order
	_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
		o.PaymentStatus = model.PaymentStatusRefunded
		o.Status = model.OrderStatusRefunded
	})

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "refunded"})
}

// HandlePaymentTimeoutCleanup cancels orders with pending payments older than a threshold.
// POST /admin/payments/timeout-cleanup
// Body: {"max_pending_minutes": 30}
// Access: admin only.

func (h *Handlers) HandlePaymentTimeoutCleanup(w http.ResponseWriter, r *http.Request) {
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
		MaxPendingMinutes int `json:"max_pending_minutes"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		req.MaxPendingMinutes = 30 // default
	}
	if req.MaxPendingMinutes <= 0 {
		req.MaxPendingMinutes = 30
	}

	result, err := h.paymentRepo.CleanupTimedOutPayments(req.MaxPendingMinutes)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":              "ok",
		"max_pending_minutes": req.MaxPendingMinutes,
		"result":              result,
	})
}

// HandleCompanyOrders returns orders containing products from a specific company.
// GET /companies/{companyId}/orders?status=...&page=...&limit=...
// Access: seller (own company) or admin.
