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

func (h *Handlers) HandleOrderCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		UserID       int64              `json:"user_id"`
		CartID       string             `json:"cart_id"`
		Items        []model.OrderItem  `json:"items"`
		ShippingInfo model.ShippingInfo `json:"shipping_info"`
		Comment      string             `json:"comment,omitempty"`
		Currency     string             `json:"currency,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	// If user is authenticated, use their ID (override request)
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && req.UserID == 0 {
		req.UserID = ctxUser.ID
	}

	var items []model.OrderItem
	var cartID string

	if req.CartID != "" {
		// Mode 1: create order from cart
		cartID = req.CartID
		if req.UserID == 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required when creating order from cart")
			return
		}

		cart, err := h.cartRepo.Get(cartID)
		if err != nil {
			httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "cart not found")
			return
		}

		if len(cart.Items) == 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "cart is empty")
			return
		}

		// Build order items from cart items
		items = make([]model.OrderItem, 0, len(cart.Items))
		for _, ci := range cart.Items {
			items = append(items, model.OrderItem{
				ProductID: ci.ProductID,
				Qty:       ci.Qty,
				Price:     ci.Price,
			})
		}
	} else {
		// Mode 2: manual items
		if req.UserID == 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required")
			return
		}
		if len(req.Items) == 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "at least one item is required")
			return
		}
		items = req.Items
	}

	// Validate and enrich items: check product exists, is active, has enough stock
	var totalAmount float64
	for i := range items {
		item := &items[i]
		if item.Qty <= 0 || item.Price <= 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid item qty or price")
			return
		}

		p, err := h.productRepo.Get(item.ProductID)
		if err != nil {
			httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("product %d not found", item.ProductID))
			return
		}
		if p.Status != model.ProductStatusActive {
			httpres.WriteError(w, http.StatusBadRequest, "PRODUCT_NOT_AVAILABLE", fmt.Sprintf("product %d is not active", item.ProductID))
			return
		}
		if p.StockQty < int64(item.Qty) {
			httpres.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_STOCK", fmt.Sprintf("product %d has only %d in stock", item.ProductID, p.StockQty))
			return
		}

		item.CompanyID = p.CompanyID
		item.Total = item.Price * float64(item.Qty)
		totalAmount += item.Total
	}

	currency := req.Currency
	if currency == "" {
		currency = "RUB"
	}

	order := &model.Order{
		UserID:        req.UserID,
		Items:         items,
		TotalAmount:   totalAmount,
		Currency:      currency,
		ShippingInfo:  req.ShippingInfo,
		Comment:       req.Comment,
		Status:        model.OrderStatusNew,
		PaymentStatus: model.PaymentStatusPending,
	}

	if err := h.orderRepo.Create(order); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Clear cart if order was created from it
	if cartID != "" {
		_ = h.cartRepo.Delete(cartID)
	}

	httpres.WriteJSON(w, http.StatusCreated, order)
}

// HandleOrderGet returns an order by ID.
// GET /orders/{id}
// Access: order owner or admin.

func (h *Handlers) HandleOrderGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "order_id")
	if !ok {
		return
	}

	order, err := h.orderRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	// Access control: only owner or admin can view
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || (order.UserID != ctxUser.ID && ctxUser.Role != model.RoleAdmin) {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only view your own orders")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, order)
}

// HandleOrderUserList returns orders for a user.
// GET /orders?user_id=123
// If user is authenticated and user_id is not provided, uses current user.

func (h *Handlers) HandleOrderUserList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	userIDStr := r.URL.Query().Get("user_id")
	var userID int64

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && userIDStr == "" {
		userID = ctxUser.ID
	} else if userIDStr != "" {
		var err error
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil || userID <= 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id")
			return
		}
	} else {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required (or authenticate)")
		return
	}

	orders, err := h.orderRepo.GetUserOrders(userID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if orders == nil {
		orders = []model.Order{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": orders,
	})
}

// HandleOrderUpdateStatus updates order status.
// PATCH /orders/{id}/status
// Body: {"status": "confirmed"}
// Access rules:
//   - admin: can change any order to any status.
//   - seller: can change only orders that contain their products (by company_id).
//   - buyer (order owner): can only cancel an order in "new" status.

func (h *Handlers) HandleOrderUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /orders/{id}/status
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "orders", "{id}", "status"]
	if len(parts) < 4 || parts[2] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing order id")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid order id")
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

	order, err := h.orderRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	newStatus := model.OrderStatus(req.Status)

	// Admin: full access
	if ctxUser.Role == model.RoleAdmin {
		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		httpres.WriteJSON(w, http.StatusOK, order)
		return
	}

	// Buyer: can only cancel their own order in "new" status
	if ctxUser.Role == model.RoleBuyer {
		if order.UserID != ctxUser.ID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage your own orders")
			return
		}
		if newStatus != model.OrderStatusCancelled {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "buyers can only cancel orders")
			return
		}
		if order.Status != model.OrderStatusNew {
			httpres.WriteError(w, http.StatusBadRequest, "INVALID_STATE", "only orders in 'new' status can be cancelled by buyer")
			return
		}
		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		httpres.WriteJSON(w, http.StatusOK, order)
		return
	}

	// Seller: can manage orders containing their products
	if ctxUser.Role == model.RoleSeller {
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil {
			httpres.WriteError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company")
			return
		}

		// Check that at least one item in the order belongs to this seller's company
		hasCompanyItem := false
		for _, item := range order.Items {
			if item.CompanyID == companyID {
				hasCompanyItem = true
				break
			}
		}
		if !hasCompanyItem {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage orders with your products")
			return
		}

		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		httpres.WriteJSON(w, http.StatusOK, order)
		return
	}

	httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
}

// ================= Payment handlers =================

// HandlePaymentCreate creates a payment for an order (stub).
// POST /payments?order_id=123
// Body: {"method": "card"}

func (h *Handlers) HandleCompanyOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /companies/{companyId}/orders
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "companies", "{companyId}", "orders"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "orders" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	// Check company exists
	company, err := h.companyRepo.Get(companyID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	// Access control: seller (own company) or admin
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if ctxUser.Role == model.RoleAdmin {
		// Admin: full access
	} else if ctxUser.Role == model.RoleSeller {
		// Seller: only own company
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || companyID != ownerCompanyID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only view orders for your own company")
			return
		}
	} else {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
		return
	}

	// Get orders
	orders, err := h.orderRepo.GetOrdersByCompanyID(companyID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Filter by status if provided
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	if statusFilter != "" {
		var filtered []model.Order
		for _, o := range orders {
			if string(o.Status) == statusFilter {
				filtered = append(filtered, o)
			}
		}
		orders = filtered
	}

	if orders == nil {
		orders = []model.Order{}
	}

	// Pagination
	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}

	total := len(orders)
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		orders = []model.Order{}
	} else if end > total {
		orders = orders[start:]
	} else {
		orders = orders[start:end]
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": companyID,
		"company":    company.Name,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"items":      orders,
	})
}

// HandleAnalyticsOrders returns aggregate order statistics.
// GET /admin/analytics/orders
// Access: admin only.
