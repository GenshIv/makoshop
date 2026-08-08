package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
)

// TurboSearchParams holds parsed turbo search parameters.
type TurboSearchParams struct {
	Q           string
	CategoryID  int64
	CompanyID   int64
	BrandID     int64
	AttrFilters map[string][]string
	Sort        string
	Page        int
	Limit       int
}

// ParseTurboSearchParams parses turbo search parameters from request.
func ParseTurboSearchParams(r *http.Request) TurboSearchParams {
	q := r.URL.Query()
	params := TurboSearchParams{
		Q:           q.Get("q"),
		Sort:        q.Get("sort"),
		Page:        1,
		Limit:       50,
		AttrFilters: make(map[string][]string),
	}

	if v := q.Get("category_id"); v != "" {
		params.CategoryID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("company_id"); v != "" {
		params.CompanyID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("brand_id"); v != "" {
		params.BrandID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("page"); v != "" {
		params.Page, _ = strconv.Atoi(v)
	}
	if v := q.Get("limit"); v != "" {
		params.Limit, _ = strconv.Atoi(v)
	}

	// Parse attr filters: attr.<code>=value1,value2
	for key, values := range q {
		if strings.HasPrefix(key, "attr.") {
			code := strings.TrimPrefix(key, "attr.")
			if code != "" {
				params.AttrFilters[code] = values
			}
		}
	}

	return params
}

// HandleTurboProducts handles GET /products/turbo
func HandleTurboProducts(turboSearch *db.TurboProductSearch, writeJSON func(http.ResponseWriter, int, interface{})) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		params := ParseTurboSearchParams(r)

		result, err := turboSearch.ListWithTurbo(db.TurboListParams{
			Q:           params.Q,
			CategoryID:  params.CategoryID,
			CompanyID:   params.CompanyID,
			BrandID:     params.BrandID,
			AttrFilters: params.AttrFilters,
			Sort:        params.Sort,
			Page:        params.Page,
			Limit:       params.Limit,
		})
		if err != nil {
			fmt.Printf("ERROR turbo search: %v\n", err)
			http.Error(w, "turbo search error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": result.Items,
			"total": result.Total,
			"page":  result.Page,
			"limit": result.Limit,
		})
	}
}
