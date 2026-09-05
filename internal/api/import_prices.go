package api

import (
	"net/http"
	"strconv"

	"github.com/GenshIv/makoshop/internal/httpres"
)

// Price import dispatcher. The legacy csv/normalized/multi import paths were
// removed — unified import (POST /admin/import-unified) covers batch imports,
// and json/nokaut remain available as explicit per-company entry points.
//
// POST /admin/import-prices
// Query params:
//   - source=json|nokaut|xml   data source (json default company params below)
//   - company=NAME_OR_ID       company for json mode
//   - limit=N                  max products (json mode)
//   - no_download=1            use the local file instead of downloading
func (h *Handlers) HandleAdminImportPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	source := r.URL.Query().Get("source")

	if source == "nokaut" || source == "xml" {
		// Nokaut XML price files (same handler as POST /admin/import-nokaut)
		h.HandleAdminImportNokaut(w, r)
		return
	}

	// json (default)
	noDownload := r.URL.Query().Get("no_download") == "1"
	companyParam := r.URL.Query().Get("company")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	h.HandleAdminImportJSON(w, r, companyParam, limit, noDownload)
}
