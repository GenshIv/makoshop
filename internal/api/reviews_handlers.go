package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

func (h *Handlers) HandleProductReviewCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /products/{id}/reviews
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "products", "{id}", "reviews"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "reviews" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	productID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || productID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	// Auth required
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	// Only buyers can review
	if ctxUser.Role != model.RoleBuyer {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "only buyers can write reviews")
		return
	}

	// Verify product exists
	_, err = h.productRepo.Get(productID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.Rating < 1 || req.Rating > 5 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "rating must be between 1 and 5")
		return
	}

	review := &model.Review{
		ProductID: productID,
		UserID:    ctxUser.ID,
		Rating:    req.Rating,
		Comment:   req.Comment,
	}

	if err := h.reviewRepo.Create(review); err != nil {
		if strings.Contains(err.Error(), "already reviewed") {
			httpres.WriteError(w, http.StatusConflict, "ALREADY_REVIEWED", err.Error())
			return
		}
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, review)
}

// HandleProductReviewsList returns reviews for a product.
// GET /products/{id}/reviews?page=1&limit=50
// Public endpoint.

func (h *Handlers) HandleProductReviewsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /products/{id}/reviews
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "reviews" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	productID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || productID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)

	reviews, total, err := h.reviewRepo.ListByProduct(productID, page, limit)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if reviews == nil {
		reviews = []model.Review{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": reviews,
	})
}

// HandleUserReviewsList returns reviews created by a user.
// GET /reviews?user_id=123
// Auth: required (user can view their own reviews)

func (h *Handlers) HandleUserReviewsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	userIDStr := r.URL.Query().Get("user_id")
	var userID int64

	if userIDStr != "" {
		var err error
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil || userID <= 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id")
			return
		}
		// Users can only view their own reviews
		if userID != ctxUser.ID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you can only view your own reviews")
			return
		}
	} else {
		userID = ctxUser.ID
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)

	reviews, total, err := h.reviewRepo.ListByUser(userID, page, limit)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if reviews == nil {
		reviews = []model.Review{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": reviews,
	})
}

// --- Admin AttrDef handlers ---

// GET /admin/attrdefs — list all attribute definitions
