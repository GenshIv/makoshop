package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// Admin maintenance operations (parts 2 and 3 of the import pipeline):
//   - POST /admin/reindex               — part 2: global index rebuild.
//   - POST /admin/eanpages/recatalogize — part 3: full verify-and-repair pass
//     over all products/pages with manual recatalogization.
//
// Both run asynchronously (the client polls GET /admin/import-progress),
// serialize on the import lock (never concurrent with an import), and pause
// makodb auto-vacuum for the duration of the run.

// beginMaintenanceJob acquires the import lock and starts a one-slot progress
// run named jobName. The import lock also serializes against shard compaction
// (background compaction holds it for its run): document bytes are zero-copy
// views into shard mmaps, and a compaction munmaps the old mapping, so a
// compaction and a bulk read must never overlap.
// Returns false (and writes the HTTP error) when another import/maintenance
// job is already running.
func (h *Handlers) beginMaintenanceJob(w http.ResponseWriter, r *http.Request, jobName, step string) bool {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return false
	}
	if !h.importMu.TryLock() {
		httpres.WriteError(w, http.StatusConflict, "JOB_IN_PROGRESS", "Another import or maintenance job is already running")
		return false
	}
	h.importProgress.Begin(1)
	h.importProgress.SetCompany(1, jobName, "")
	h.importProgress.SetStep(step)
	return true
}

// runMaintenance wraps a maintenance goroutine body with the mandatory
// bookkeeping: restore auto-vacuum, finalize the progress tracker and, for
// jobs other than compaction itself, queue a follow-up shard compaction
// (bulk jobs run with auto-vacuum paused and pile up dead bytes).
func (h *Handlers) runMaintenance(body func() error) {
	h.runMaintenanceOpts(body, true)
}

func (h *Handlers) runMaintenanceOpts(body func() error, compactAfter bool) {
	if h.store != nil {
		h.store.DB().SetAutoVacuum(false)
	}
	err := body()
	if h.store != nil {
		h.store.DB().SetAutoVacuum(true)
	}
	if err != nil {
		fmt.Printf("[MAINTENANCE] failed: %v\n", err)
		h.importProgress.Fail(err.Error())
	} else {
		h.importProgress.CompanyDone(1, CompanyStateCompleted, "")
	}
	h.importProgress.Finish()
	h.importMu.Unlock()

	if compactAfter {
		// The goroutine takes the import lock, so it starts only when no
		// other job is running, and jobs started while it compacts queue up
		// behind it.
		h.compactInBackground()
	}
}

// compactInBackground reclaims dead bytes in all shards. It holds the import
// lock for the whole run: compaction munmaps old shard mmaps, and document
// bytes are zero-copy views into them, so a compaction and a bulk read
// (import / reindex / recatalogize) must never overlap. Everything job-like
// in this server is serialized through that one lock.
func (h *Handlers) compactInBackground() {
	if h.store == nil {
		return
	}
	go func() {
		h.importMu.Lock()
		defer h.importMu.Unlock()
		start := time.Now()
		fmt.Println("[DB] compacting shards (reclaiming dead bytes)...")
		if err := h.store.DB().CompactAllShards(20000); err != nil {
			fmt.Printf("[DB] WARN: compaction failed: %v\n", err)
			return
		}
		fmt.Printf("[DB] compaction done in %v\n", time.Since(start))
	}()
}

// HandleAdminReindex is part 2 as a standalone button: global rebuild of the
// derived state — page counts, min prices, catalog/product sort indexes and
// category trees. No product data is touched.
//
// POST /admin/reindex
func (h *Handlers) HandleAdminReindex(w http.ResponseWriter, r *http.Request) {
	if !h.beginMaintenanceJob(w, r, "Global reindex", StepSortIndexes) {
		return
	}

	go h.runMaintenance(func() error {
		start := time.Now()
		fmt.Println("[REINDEX] Starting global index rebuild...")
		if err := h.runGlobalRecalculation(nil); err != nil {
			return err
		}
		fmt.Printf("[REINDEX] Global index rebuild done in %v\n", time.Since(start))
		return nil
	})

	httpres.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"message": "Global reindex started in the background. Poll /admin/import-progress for status.",
	})
}

// HandleAdminCompact compacts all shards: rewrites them without dead bytes
// left by rewrite-heavy operations (index rebuilds, imports with paused
// auto-vacuum). Cures "makodb: out of database space" without raising the
// shard budget.
//
// POST /admin/compact
func (h *Handlers) HandleAdminCompact(w http.ResponseWriter, r *http.Request) {
	if !h.beginMaintenanceJob(w, r, "Compact DB", "compact") {
		return
	}

	go h.runMaintenanceOpts(func() error {
		start := time.Now()
		fmt.Println("[DB] manual compaction started...")
		if err := h.store.DB().CompactAllShards(20000); err != nil {
			return fmt.Errorf("compact shards: %w", err)
		}
		fmt.Printf("[DB] manual compaction done in %v\n", time.Since(start))
		return nil
	}, false)

	httpres.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"message": "Compaction started in the background. Poll /admin/import-progress for status.",
	})
}

// HandleAdminEANPageRecatalogize is part 3: walk ALL products, group them into
// pages, and verify every page is in place. Only actual differences are
// written (skip-unchanged), and the whole diff is applied in ONE makodb
// transaction: page updates/creates swap atomically at a single commit.
// After the commit: orphan page cleanup, delivery attribute pass and the
// part-2 tail (sort indexes + trees).
//
// Verified per page: existence, product count, min price and the category —
// the catalogizer top match overwrites the assigned category when they
// disagree (that is the point of a manual recatalogization).
//
// POST /admin/eanpages/recatalogize
func (h *Handlers) HandleAdminEANPageRecatalogize(w http.ResponseWriter, r *http.Request) {
	if !h.beginMaintenanceJob(w, r, "Recatalogize", StepProducts) {
		return
	}

	go h.runMaintenance(func() error {
		return h.runRecatalogize()
	})

	httpres.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"message": "Recatalogize started in the background. Poll /admin/import-progress for status.",
	})
}

// runRecatalogize performs the verify-and-repair pass.
func (h *Handlers) runRecatalogize() error {
	start := time.Now()

	// 1. Walk all products and group them by page key.
	h.importProgress.SetStep(StepProducts)
	allProducts, err := h.productRepo.GetAllProducts()
	if err != nil {
		return fmt.Errorf("get all products: %w", err)
	}
	type group struct {
		count      int
		minPrice   float64
		currency   string
		companyIDs map[int64]struct{}
	}
	groups := make(map[string]*group, len(allProducts))
	for i := range allProducts {
		p := &allProducts[i]
		key := db.EANPageKeyForProduct(p)
		if key == "" {
			continue
		}
		g, ok := groups[key]
		if !ok {
			g = &group{count: 0, minPrice: p.Price, currency: p.Currency, companyIDs: make(map[int64]struct{})}
			groups[key] = g
		}
		g.count++
		g.companyIDs[p.CompanyID] = struct{}{}
		if p.Price > 0 && (g.minPrice == 0 || p.Price < g.minPrice) {
			g.minPrice = p.Price
			g.currency = p.Currency
		}
	}
	fmt.Printf("[RECATALOGIZE] %d products in %d page groups\n", len(allProducts), len(groups))

	// Company delivery slugs, resolved once per company (the delivery_method
	// attribute is company-derived; see companyDeliverySlugs).
	deliveryByCompany := make(map[int64][]string)
	deliveryFor := func(companyIDs map[int64]struct{}) map[string]struct{} {
		slugs := make(map[string]struct{})
		for cid := range companyIDs {
			if _, ok := deliveryByCompany[cid]; !ok {
				if c, err := h.companyRepo.Get(cid); err == nil && c != nil {
					deliveryByCompany[cid] = h.companyDeliverySlugs(c)
				} else {
					deliveryByCompany[cid] = nil
				}
			}
			for _, slug := range deliveryByCompany[cid] {
				slugs[slug] = struct{}{}
			}
		}
		return slugs
	}

	// 2. Load all pages and diff them against the groups.
	h.importProgress.SetStep(StepEANPages)
	allPages, err := h.eanPageRepo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	txn := db.NewTransaction(h.store)
	if err := txn.Begin(); err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if !txn.IsFinished() {
			_ = txn.Abort()
		}
	}()

	// Page lookup by group key.
	pageByKey := make(map[string]*model.EANPage, len(allPages))
	for i := range allPages {
		pageByKey[allPages[i].EAN] = &allPages[i]
	}

	// Groups with no page at all: products exist but the page is gone.
	// Recreate them via the regular upsert (reuses title/slug/keywords/images
	// logic and auto-catalogization).
	var missingProducts []*model.Product
	for key := range groups {
		if _, ok := pageByKey[key]; ok {
			continue
		}
		for j := range allProducts {
			p := &allProducts[j]
			if db.EANPageKeyForProduct(p) == key {
				missingProducts = append(missingProducts, p)
			}
		}
	}

	updated, unchanged := 0, 0
	treePathCache := make(map[int64][]string)

	// Catalogizer token sets loaded ONCE for the whole pass: scoring a page
	// is in-memory instead of re-reading every category's token index per
	// page (which is O(pages x categories) index reads).
	var catSets map[int64]map[uint64]struct{}
	if h.catalogizer != nil {
		catSets = h.eanPageRepo.CatalogTokenSets()
		fmt.Printf("[RECATALOGIZE] catalog token sets loaded: %d categories\n", len(catSets))
	}

	for i := range allPages {
		sp := &allPages[i]
		g, ok := groups[sp.EAN]

		pageChanged := false

		// Product count. EAN pages are NEVER deleted (SEO): a page without
		// products stays alive with zero offers — accessible by URL, excluded
		// from catalog sort indexes.
		newCount := 0
		if ok {
			newCount = g.count
		}
		if sp.ProductCount != newCount {
			sp.ProductCount = newCount
			pageChanged = true
		}
		if !ok {
			g = nil // no products: min-price/category/delivery checks below degrade gracefully
		}
		// Min price (and its currency).
		if g != nil && g.minPrice > 0 && sp.MinPrice != g.minPrice {
			sp.MinPrice = g.minPrice
			if g.currency != "" {
				sp.Currency = g.currency
			}
			pageChanged = true
		}
		// Category: catalogizer top match overwrites on disagreement.
		if len(catSets) > 0 {
			text := sp.Keywords
			if text == "" {
				text = sp.Title
			}
			if bestCat := db.BestCategoryByText(text, catSets); bestCat != 0 && bestCat != sp.CategoryID {
				sp.CategoryID = bestCat
				sp.SeoURL = h.eanPageRepo.ComputeSeoURL(sp.Slug, sp.CategoryID, treePathCache)
				pageChanged = true
			}
		}
		// Delivery: attribute must match the importing companies' settings
		// (empty set for pages without products — attribute cleared).
		var expectedSlugs map[string]struct{}
		if g != nil {
			expectedSlugs = deliveryFor(g.companyIDs)
		}
		if !db.SameStringSet(db.DeliveryMethodSlugsOf(sp.Attributes), expectedSlugs) {
			sp.Attributes = db.SetDeliveryMethodAttr(sp.Attributes, expectedSlugs)
			pageChanged = true
		}

		if !pageChanged {
			unchanged++
			continue
		}
		sp.UpdatedAt = time.Now().Unix()
		data := db.MarshalEANPage(*sp)
		if err := txn.DocPut(db.KeyEANPage(sp.ID), data); err != nil {
			fmt.Printf("[RECATALOGIZE] WARN: buffer page %d: %v\n", sp.ID, err)
			continue
		}
		updated++
		if updated%10000 == 0 {
			fmt.Printf("[RECATALOGIZE] buffered %d page updates...\n", updated)
		}
	}

	// Missing pages: products exist but no page. Reuse the regular upsert so
	// creation matches the import path (incl. auto-catalogization).
	h.eanPageRepo.LoadCatalogizerCache()
	if len(missingProducts) > 0 {
		_, createdPages := h.eanPageRepo.BatchUpsertFromProductsTx(txn, missingProducts, nil)
		if h.eanPageSearch != nil && len(createdPages) > 0 {
			if err := h.eanPageSearch.IndexEANPageBatchTx(txn, createdPages); err != nil {
				return fmt.Errorf("index recreated pages: %w", err)
			}
		}
	}

	// 3. One commit swaps all buffered page updates.
	h.importProgress.SetStep(StepCommit)
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Printf("[RECATALOGIZE] committed: %d pages updated, %d unchanged, %d recreated\n",
		updated, unchanged, len(missingProducts))

	// 4. Full index rebuild from scratch: every index key is written ONCE
	// with its complete content (RebuildAllIndexes accumulates everything and
	// flushes a single write per key; sort indexes go through its
	// transactional builder). Pages without products are simply absent from
	// the rebuilt catalog indexes — no incremental unindexing churn — while
	// their docs stay alive for SEO.
	h.importProgress.SetStep(StepSortIndexes)
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.RebuildAllIndexes(); err != nil {
			return err
		}
	}
	if h.turboSearch != nil {
		if err := h.turboSearch.BuildSortIndexes(); err != nil {
			return fmt.Errorf("build product sort indexes: %w", err)
		}
	}
	if h.categoryRepo != nil {
		h.categoryRepo.RebuildTrees()
	}

	fmt.Printf("[RECATALOGIZE] done in %v (updated=%d unchanged=%d recreated=%d)\n",
		time.Since(start), updated, unchanged, len(missingProducts))
	return nil
}
