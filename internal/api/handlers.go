package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GenshIv/makoshop/internal/catalogizer"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/stats"
	"github.com/GenshIv/silentjson/v2"
)

type Handlers struct {
	store             *db.Store
	categoryRepo      *db.CategoryRepo
	attrDefRepo       *db.AttrDefRepo
	productRepo       *db.ProductRepo
	turboSearch       *db.TurboProductSearch
	scuPageSearch     *db.SCUPageSearch
	landingRepo       *db.LandingRepo
	scuPageRepo       *db.SCUPageRepo
	companyRepo       *db.CompanyRepo
	userRepo          *db.UserRepo
	cartRepo          *db.CartRepo
	orderRepo         *db.OrderRepo
	paymentRepo       *db.PaymentRepo
	reviewRepo        *db.ReviewRepo
	productImportRepo *db.ProductImportRepo
	promoPlanRepo     *db.PromoPlanRepo
	promoCampaignRepo *db.PromoCampaignRepo
	promoLogRepo      *db.PromoLogRepo
	catalogizer       *catalogizer.Catalogizer

	// Company settings repos
	paymentMethodRepo   *db.PaymentMethodRepo
	deliveryTimeRepo    *db.DeliveryTimeRepo
	installmentPlanRepo *db.InstallmentPlanRepo

	// Stats cache
	statsCacheMu sync.Mutex
	statsCache   *metrics.Stats
	statsCacheAt time.Time

	// Category attrs cache: catID -> []db.AttrItem
	// Cached for 5 minutes, invalidated on attr/category changes via admin.
	catAttrsMu sync.RWMutex
	catAttrs   map[int64]cachedCatAttrs

	// Stats collector
	statsCollector *stats.StatsCollector
}

// cachedCatAttrs holds precomputed attribute items for a category.
type cachedCatAttrs struct {
	Items []db.AttrItem
	At    time.Time
}

func NewHandlers(store *db.Store) *Handlers {
	promoCampaignRepo := db.NewPromoCampaignRepo(store)
	promoPlanRepo := db.NewPromoPlanRepo(store)
	promoLogRepo := db.NewPromoLogRepo(store)
	productRepo := db.NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)
	categoryRepo := db.NewCategoryRepo(store)

	// Build precomputed category tree JSONs at startup.
	categoryRepo.RebuildTrees()

	attrDefRepo := db.NewAttrDefRepo(store)

	// Turbo search: enabled by default. Can be disabled via env flag if needed.
	turboEnabled := true
	turboSearch := db.NewTurboProductSearch(store, productRepo, categoryRepo, turboEnabled)
	productRepo.SetTurboSearch(turboSearch)

	landingRepo := db.NewLandingRepo(store)
	turboSearch.SetLandingRepo(landingRepo)

	scuPageRepo := db.NewSCUPageRepo(store)
	scuPageRepo.SetCategoryRepo(categoryRepo)
	scuPageRepo.EnableCatalogizeNew(false) // disabled: categories come from price files
	turboSearch.SetSCUPageRepo(scuPageRepo)

	// SCUPage search (catalog works on SCU pages)
	scuPageSearch := db.NewSCUPageSearch(store.DB(), scuPageRepo, productRepo, categoryRepo, turboEnabled)
	productRepo.SetSCUPageSearch(scuPageSearch)

	// Catalogizer
	catz := catalogizer.New(store, categoryRepo, productRepo)
	scuPageRepo.Catalogizer = catz // for TurboTopNByIntersection catalogization

	// Stats collector with persistence
	statsConfig := stats.DefaultStatsConfig()
	statsCollector := stats.NewStatsCollectorWithPersistence(
		statsConfig,
		1000,
		"stats:visits",
		store.DB(),
	)

	return &Handlers{
		store:             store,
		categoryRepo:      categoryRepo,
		attrDefRepo:       attrDefRepo,
		companyRepo:       db.NewCompanyRepo(store),
		userRepo:          db.NewUserRepo(store),
		cartRepo:          db.NewCartRepo(store),
		orderRepo:         db.NewOrderRepo(store),
		paymentRepo:       db.NewPaymentRepo(store),
		reviewRepo:        db.NewReviewRepo(store),
		productImportRepo: db.NewProductImportRepo(store, productRepo),
		promoPlanRepo:     promoPlanRepo,
		promoCampaignRepo: promoCampaignRepo,
		promoLogRepo:      promoLogRepo,
		productRepo:       productRepo,
		turboSearch:       turboSearch,
		scuPageSearch:     scuPageSearch,
		landingRepo:       landingRepo,
		scuPageRepo:       scuPageRepo,
		catalogizer:       catz,
		catAttrs:          make(map[int64]cachedCatAttrs),
		statsCollector:    statsCollector,
	}
}

// Store returns the underlying store.
func (h *Handlers) Store() *db.Store {
	return h.store
}

// TurboSearch returns the attached TurboProductSearch.
func (h *Handlers) TurboSearch() *db.TurboProductSearch {
	return h.turboSearch
}

// StatsCollector returns the attached stats collector.
func (h *Handlers) StatsCollector() *stats.StatsCollector {
	return h.statsCollector
}

// SetCompanySettingsRepos attaches company settings repositories.
func (h *Handlers) SetCompanySettingsRepos(
	companyRepo *db.CompanyRepo,
	paymentMethodRepo *db.PaymentMethodRepo,
	deliveryTimeRepo *db.DeliveryTimeRepo,
	installmentPlanRepo *db.InstallmentPlanRepo,
) {
	h.companyRepo = companyRepo
	h.paymentMethodRepo = paymentMethodRepo
	h.deliveryTimeRepo = deliveryTimeRepo
	h.installmentPlanRepo = installmentPlanRepo
}

// InvalidateCatAttrsCache clears cached attrs for a category (called on attr/category changes).
func (h *Handlers) InvalidateCatAttrsCache(catID int64) {
	h.catAttrsMu.Lock()
	delete(h.catAttrs, catID)
	h.catAttrsMu.Unlock()
}

// GetCategoryAttrs returns cached category attributes (TTL 5 min).
func (h *Handlers) GetCategoryAttrs(catID int64) []db.AttrItem {
	if catID == 0 || h.attrDefRepo == nil {
		return nil
	}

	// Check cache first
	h.catAttrsMu.RLock()
	cached, ok := h.catAttrs[catID]
	h.catAttrsMu.RUnlock()
	if ok && time.Since(cached.At) < 5*time.Minute {
		return cached.Items
	}

	// Build attrs
	codes, err := h.attrDefRepo.GetCodesForCategoryTree(catID, h.categoryRepo)
	if err != nil || len(codes) == 0 {
		return nil
	}

	items := make([]db.AttrItem, 0, len(codes))
	for _, code := range codes {
		values, _ := h.attrDefRepo.GetAttrValuesForCategory(code, catID)
		if len(values) == 0 {
			continue
		}
		def, _ := h.attrDefRepo.GetByCode(code)
		attrMap := db.AttrItem{
			Code:    code,
			Options: values,
		}
		if def != nil {
			attrMap.NameRU = def.NameRu
			attrMap.NameUA = def.NameUa
			attrMap.NamePL = def.NamePl
			attrMap.NameEN = def.NameEn
			attrMap.Type = string(def.Type)
			attrMap.IsFilterable = def.IsFilterable
		} else {
			attrMap.Type = "string"
			attrMap.IsFilterable = true
		}
		items = append(items, attrMap)
	}

	// Store in cache
	if len(items) > 0 {
		h.catAttrsMu.Lock()
		h.catAttrs[catID] = cachedCatAttrs{Items: items, At: time.Now()}
		h.catAttrsMu.Unlock()
	}

	return items
}

// --- helpers ---

var jsonBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 64*1024) // 64KB initial
		return &buf
	},
}

// marshalWithPool serializes data using silentjson with a pooled buffer.
// Returns the resulting JSON bytes (caller owns the returned slice, not the pool buffer).
func marshalWithPool[T any](data *T, reg *silentjson.Registry) []byte {
	bufPtr := jsonBufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	buf = silentjson.Marshal(data, reg, buf)
	// Copy result before returning buf to pool
	result := make([]byte, len(buf))
	copy(result, buf)
	*bufPtr = buf[:64*1024] // reset capacity for reuse
	jsonBufPool.Put(bufPtr)
	return result
}

// writeJSONWithPool marshals data directly to w without extra copy.
// Optimized for hot paths where we only need to write, not keep the JSON.
func writeJSONWithPool[T any](w http.ResponseWriter, data *T, reg *silentjson.Registry) {
	bufPtr := jsonBufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	buf = silentjson.Marshal(data, reg, buf)

	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(buf)
	*bufPtr = buf[:64*1024] // reset capacity for reuse
	jsonBufPool.Put(bufPtr)
}

func writeJSONSCUList(w http.ResponseWriter, r *http.Request, status int, data db.SCUListRespData) {
	w.Header().Set("Content-Type", "application/json")
	if r != nil && r.Method == http.MethodHead {
		// For HEAD, we want the Content-Length but no body.
		// However, marshalWithPool/writeJSONWithPool is needed to know the length.
		bufPtr := jsonBufPool.Get().(*[]byte)
		buf := (*bufPtr)[:0]
		buf = silentjson.Marshal(&data, scuListRespRegistry, buf)
		w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
		w.WriteHeader(status)
		*bufPtr = buf[:64*1024]
		jsonBufPool.Put(bufPtr)
		return
	}
	writeJSONWithPool(w, &data, scuListRespRegistry)
}

// HandleTurboProducts handles GET /products/turbo (turbo-index based search).
// HEAD /products/turbo — returns headers only, body is not sent.
func parseID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	idStr := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if idStr == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing id")
		return 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return 0, false
	}
	return id, true
}

func parseQueryInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseQueryInt(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def, err
	}
	return v, nil
}

func parseQueryFloat(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseQueryFloatPtr(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// --- Categories ---

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
