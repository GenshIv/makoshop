package router

import (
	"net/http"
	"strings"
)

// adminProductCategory handles /admin/products/{ean}/assign-category and
// /admin/products/{ean}/category-mapping.
func (d *Deps) adminProductCategory(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin/products/")

	// Find the /assign-category or /category-mapping segment
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	ean := segments[0]
	action := segments[1]

	switch action {
	case "assign-category":
		d.Handlers.HandleProductAssignCategory(w, r, ean)
	case "category-mapping":
		d.Handlers.HandleProductGetCategoryMapping(w, r, ean)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}
