package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// HandleAdminReviewsList returns paginated reviews for admin.
// GET /admin/reviews?page=1&limit=50&status=approved&e=123456789
func (h *Handlers) HandleAdminReviewsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	_, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)
	statusFilter := r.URL.Query().Get("status")
	eanFilter := r.URL.Query().Get("e")

	reviews, total, err := h.reviewRepo.ListAll(page, limit, statusFilter, eanFilter)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if reviews == nil {
		reviews = []model.Review{}
	}

	// Enrich with user names
	type ReviewWithUser struct {
		model.Review
		UserName string `json:"user_name,omitempty"`
	}

	var enriched []ReviewWithUser
	for _, rev := range reviews {
		user, _ := h.userRepo.GetByID(rev.UserID)
		reviewWithUser := ReviewWithUser{Review: rev}
		if user != nil {
			reviewWithUser.UserName = user.Profile.Name
			if reviewWithUser.UserName == "" {
				reviewWithUser.UserName = user.Email
			}
		}
		enriched = append(enriched, reviewWithUser)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": enriched,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// HandleAdminReviewGet returns a single review by ID.
// GET /admin/reviews/{id}
func (h *Handlers) HandleAdminReviewGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}

	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	review, err := h.reviewRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Enrich with user name
	user, _ := h.userRepo.GetByID(review.UserID)
	response := map[string]interface{}{
		"id":          review.ID,
		"product_id":  review.ProductID,
		"ean":         review.EAN,
		"ean_page_id": review.EANPageID,
		"user_id":     review.UserID,
		"user_name":   "",
		"rating":      review.Rating,
		"comment":     review.Comment,
		"status":      string(review.Status),
		"is_featured": review.IsFeatured,
		"verified":    review.Verified,
		"created_at":  review.CreatedAt,
		"updated_at":  review.UpdatedAt,
	}

	if user != nil {
		response["user_name"] = user.Profile.Name
		if response["user_name"] == "" {
			response["user_name"] = user.Email
		}
	}

	httpres.WriteJSON(w, http.StatusOK, response)
}

// HandleAdminReviewUpdate updates a review (moderation).
// PATCH /admin/reviews/{id}
func (h *Handlers) HandleAdminReviewUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}

	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	var updates map[string]interface{}
	if !httpres.ReadJSON(w, r, &updates) {
		return
	}

	review, err := h.reviewRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Apply updates
	if v, ok := updates["status"].(string); ok {
		newStatus := model.ReviewStatus(v)
		if newStatus != review.Status {
			if err := h.reviewRepo.UpdateStatus(id, newStatus); err != nil {
				httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			// Recalculate product rating after status change
			h.recalculateProductRating(review.ProductID)
		}
	}

	if v, ok := updates["is_featured"].(bool); ok {
		if v != review.IsFeatured {
			if err := h.reviewRepo.UpdateFeatured(id, v); err != nil {
				httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
		}
	}

	// Return updated review
	updated, _ := h.reviewRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

// HandleAdminReviewDelete deletes a review.
// DELETE /admin/reviews/{id}
func (h *Handlers) HandleAdminReviewDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}

	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	review, err := h.reviewRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	if err := h.reviewRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Recalculate product rating after deletion
	h.recalculateProductRating(review.ProductID)

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleAdminReviewsBulkActions performs bulk actions on reviews.
// POST /admin/reviews/bulk-actions
func (h *Handlers) HandleAdminReviewsBulkActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Action string  `json:"action"` // "approve", "reject", "hide", "delete"
		IDs    []int64 `json:"ids"`
	}

	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if len(req.IDs) == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "ids are required")
		return
	}

	processed := 0
	var errors []string

	for _, id := range req.IDs {
		switch req.Action {
		case "approve":
			if err := h.reviewRepo.UpdateStatus(id, model.ReviewStatusApproved); err != nil {
				errors = append(errors, fmt.Sprintf("review %d: %v", id, err))
			} else {
				processed++
			}
		case "reject":
			if err := h.reviewRepo.UpdateStatus(id, model.ReviewStatusRejected); err != nil {
				errors = append(errors, fmt.Sprintf("review %d: %v", id, err))
			} else {
				processed++
			}
		case "hide":
			if err := h.reviewRepo.UpdateStatus(id, model.ReviewStatusHidden); err != nil {
				errors = append(errors, fmt.Sprintf("review %d: %v", id, err))
			} else {
				processed++
			}
		case "delete":
			review, err := h.reviewRepo.Get(id)
			if err != nil {
				errors = append(errors, fmt.Sprintf("review %d: %v", id, err))
				continue
			}
			if err := h.reviewRepo.Delete(id); err != nil {
				errors = append(errors, fmt.Sprintf("review %d: %v", id, err))
			} else {
				processed++
				h.recalculateProductRating(review.ProductID)
			}
		default:
			errors = append(errors, fmt.Sprintf("unknown action: %s", req.Action))
		}
	}

	resp := map[string]interface{}{
		"processed": processed,
		"total":     len(req.IDs),
	}
	if len(errors) > 0 {
		resp["errors"] = errors
	}

	httpres.WriteJSON(w, http.StatusOK, resp)
}

// HandleAdminReviewStats returns review statistics.
// GET /admin/reviews/stats
func (h *Handlers) HandleAdminReviewStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	stats, err := h.reviewRepo.GetStats()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, stats)
}

// HandleAdminReviewsRecalculate recalculates ratings for all products.
// POST /admin/reviews/recalculate
func (h *Handlers) HandleAdminReviewsRecalculate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Get all products
	products, err := h.productRepo.GetAllProducts()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	updated := 0
	for _, p := range products {
		avg, count, err := h.reviewRepo.RecalculateProductRating(p.ID)
		if err != nil {
			continue
		}

		if avg != p.AvgRating || count != p.ReviewCount {
			if err := h.productRepo.Update(p.ID, func(prod *model.Product) {
				prod.AvgRating = avg
				prod.ReviewCount = count
			}); err != nil {
				continue
			}
			updated++
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "completed",
		"products_updated": updated,
		"total_products":   len(products),
	})
}

// recalculateProductRating is a helper that recalculates and updates a product's rating.
func (h *Handlers) recalculateProductRating(productID int64) {
	avg, count, err := h.reviewRepo.RecalculateProductRating(productID)
	if err != nil {
		fmt.Printf("[REVIEW] recalculate rating for product %d: %v\n", productID, err)
		return
	}

	if err := h.productRepo.Update(productID, func(p *model.Product) {
		p.AvgRating = avg
		p.ReviewCount = count
	}); err != nil {
		fmt.Printf("[REVIEW] update product %d rating: %v\n", productID, err)
	}
}

// HandleSellerReviews returns reviews for a seller's products.
// GET /seller/reviews?product_id=123
func (h *Handlers) HandleSellerReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if ctxUser.Role != model.RoleSeller {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "seller access required")
		return
	}

	companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
	if err != nil || companyID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company")
		return
	}

	// Get product IDs for this company via turbo search
	var productIDs []int64
	if h.turboSearch != nil {
		result, err := h.turboSearch.ListWithTurbo(db.TurboListParams{
			CompanyID: companyID,
			Page:      1,
			Limit:     1000,
		})
		if err == nil && len(result.Items) > 0 {
			for _, item := range result.Items {
				var m map[string]interface{}
				if err := json.Unmarshal(item, &m); err != nil {
					continue
				}
				if id, ok := m["id"].(float64); ok {
					productIDs = append(productIDs, int64(id))
				}
			}
		}
	}

	var allReviews []model.Review
	for _, pid := range productIDs {
		reviews, _, err := h.reviewRepo.ListByProduct(pid, 1, 100, "")
		if err != nil {
			continue
		}
		allReviews = append(allReviews, reviews...)
	}

	// Sort by created_at desc
	for i := len(allReviews)/2 - 1; i >= 0; i-- {
		j := len(allReviews) - 1 - i
		if allReviews[i].CreatedAt < allReviews[j].CreatedAt {
			allReviews[i], allReviews[j] = allReviews[j], allReviews[i]
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items":   allReviews,
		"total":   int64(len(allReviews)),
		"company": companyID,
	})
}
