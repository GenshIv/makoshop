package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/httpres"
)

func (h *Handlers) HandleCartCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		UserID    *int64 `json:"user_id,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	// If user is authenticated, use their ID (override request)
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && req.UserID == nil {
		req.UserID = &ctxUser.ID
	}

	cart, err := h.cartRepo.Create(req.UserID, req.SessionID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, cart)
}

// HandleCartGet returns a cart by ID.
// GET /cart/{id}

func (h *Handlers) HandleCartGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if cartID == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}

	cart, err := h.cartRepo.Get(cartID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "cart not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, cart)
}

// HandleCartMe returns the current user's cart.
// GET /cart/me (requires auth)
// If user has no cart, creates one automatically.

func (h *Handlers) HandleCartMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	cart, err := h.cartRepo.GetUserCart(ctxUser.ID)
	if err != nil {
		// No cart yet — create one
		cart, err = h.cartRepo.CreateForUser(ctxUser.ID)
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create cart")
			return
		}
	}

	httpres.WriteJSON(w, http.StatusOK, cart)
}

// HandleCartItemAdd adds a product to the cart.
// POST /cart/{id}/items
// Body: {"product_id": 123, "qty": 2}

func (h *Handlers) HandleCartItemAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /cart/{id}/items
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "cart", "{id}", "items"]
	if len(parts) < 4 || parts[2] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}
	cartID := parts[2]

	var req struct {
		ProductID int64 `json:"product_id"`
		Qty       int   `json:"qty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.ProductID == 0 || req.Qty <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "product_id and qty > 0 required")
		return
	}

	// Get product to fetch price and name
	p, err := h.productRepo.Get(req.ProductID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	cart, err := h.cartRepo.AddItem(cartID, req.ProductID, p.Name, req.Qty, p.Price)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, cart)
}

// HandleCartItemUpdate updates quantity of an item in the cart.
// PATCH /cart/{id}/items/{product_id}
// Body: {"qty": 5} (qty=0 removes the item)

func (h *Handlers) HandleCartItemUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /cart/{cart_id}/items/{product_id}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "cart", "{cart_id}", "items", "{product_id}"]
	if len(parts) < 5 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	cartID := parts[2]
	productIDStr := parts[4]

	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	var req struct {
		Qty int `json:"qty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	cart, err := h.cartRepo.UpdateItem(cartID, productID, req.Qty)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	httpres.WriteJSON(w, http.StatusOK, cart)
}

// HandleCartDelete deletes a cart.
// DELETE /cart/{id}

func (h *Handlers) HandleCartDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if cartID == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}

	if err := h.cartRepo.Delete(cartID); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ================= Order handlers =================

// HandleOrderCreate creates a new order.
// POST /orders
// Body (two modes):
//  1. From cart: {"cart_id": "...", "shipping_info": {...}, "comment": "..."}
//  2. Manual:    {"user_id": 1, "items": [...], "shipping_info": {...}, "comment": "..."}
//
// user_id can be omitted if user is authenticated.
// If cart_id is provided, items are taken from the cart and the cart is cleared.
