package api

import (
	"net/http"
	"sort"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/httpres"
	// "github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/model"
)

func (h *Handlers) HandleAnalyticsOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Calculate aggregates
	totalOrders := len(orders)
	totalRevenue := 0.0
	statusCounts := make(map[string]int)
	paymentStatusCounts := make(map[string]int)

	for _, o := range orders {
		statusCounts[string(o.Status)]++
		paymentStatusCounts[string(o.PaymentStatus)]++
		totalRevenue += o.TotalAmount
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"total_orders":      totalOrders,
		"total_revenue":     totalRevenue,
		"by_status":         statusCounts,
		"by_payment_status": paymentStatusCounts,
	})
}

// HandleAnalyticsOverview returns general platform metrics.
// GET /admin/analytics/overview
// Access: admin only.

func (h *Handlers) HandleAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	return
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Count users
	users, err := h.userRepo.GetAllUsers()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Count companies
	companies, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Count products
	products, err := h.productRepo.GetAllProducts()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Orders and revenue
	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	totalOrders := len(orders)
	totalRevenue := 0.0
	for _, o := range orders {
		if o.Status != model.OrderStatusCancelled && o.Status != model.OrderStatusRefunded {
			totalRevenue += o.TotalAmount
		}
	}

	// Promo revenue (budget_used across all campaigns)
	promoCampaigns, err := h.promoCampaignRepo.ListAll()
	if err != nil {
		promoCampaigns = []model.PromoCampaign{}
	}

	var promoRevenue float64
	for _, c := range promoCampaigns {
		promoRevenue += c.BudgetUsed
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"total_users":     len(users),
		"total_companies": len(companies),
		"total_products":  len(products),
		"total_orders":    totalOrders,
		"total_revenue":   totalRevenue,
		"promo_revenue":   promoRevenue,
	})
}

// HandleAnalyticsProducts returns popular products.
// GET /admin/analytics/products?from=...&to=...&limit=10&sort=orders
// Access: admin only.
// sort: views|orders|revenue (default: orders)

func (h *Handlers) HandleAnalyticsProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 10)
	if limit > 100 {
		limit = 100
	}

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "orders"
	}

	// Collect product stats from orders
	type ProductStats struct {
		ProductID int64
		Name      string
		Orders    int
		Revenue   float64
	}

	statsMap := make(map[int64]*ProductStats)

	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	for _, o := range orders {
		if o.Status == model.OrderStatusCancelled || o.Status == model.OrderStatusRefunded {
			continue
		}

		for _, item := range o.Items {
			if _, ok := statsMap[item.ProductID]; !ok {
				statsMap[item.ProductID] = &ProductStats{ProductID: item.ProductID}
			}
			s := statsMap[item.ProductID]
			s.Orders += item.Qty
			s.Revenue += item.Total
		}
	}

	// Enrich with product names
	for id, s := range statsMap {
		p, err := h.productRepo.Get(id)
		if err == nil {
			s.Name = p.Name
		}
	}

	// Convert to slice
	var stats []*ProductStats
	for _, s := range statsMap {
		stats = append(stats, s)
	}

	// Sort
	switch sortBy {
	case "orders":
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Orders > stats[j].Orders
		})
	case "revenue":
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Revenue > stats[j].Revenue
		})
	case "views":
		// Views tracking not implemented yet; fallback to orders
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Orders > stats[j].Orders
		})
	}

	// Limit
	if len(stats) > limit {
		stats = stats[:limit]
	}

	result := make([]map[string]interface{}, 0, len(stats))
	for _, s := range stats {
		result = append(result, map[string]interface{}{
			"product_id": s.ProductID,
			"name":       s.Name,
			"orders":     s.Orders,
			"revenue":    s.Revenue,
			"views":      0, // TODO: implement views tracking
		})
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": result,
	})
}

// HandleAnalyticsSearchQueries returns popular search queries.
// GET /admin/analytics/search-queries?from=...&to=...&limit=20
// Access: admin only.
// NOTE: Currently returns stub data (search query logging not yet implemented).

func (h *Handlers) HandleAnalyticsSearchQueries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// TODO: implement search query logging and aggregation
	// For now, return empty list with a note
	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": []interface{}{},
		"note":  "search query logging not yet implemented",
	})
}

// ================= Promo handlers =================

// HandlePromoPlansList returns all promo plans (public).
// GET /promo/plans
