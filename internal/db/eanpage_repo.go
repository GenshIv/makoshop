package db

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/slug"
	"github.com/GenshIv/makoshop/internal/tokenizer"
)

const (
	TurboKeyEANPageList = "eanpage_list"
	turboKeyEANPageEAN  = "eanpage_ean:"  // prefix for EAN lookup
	turboKeyEANPageSlug = "eanpage_slug:" // prefix for slug lookup

	// Limits to prevent EAN pages from growing too large in DB
	maxEANPageProductIDs = 500 // max product IDs to store in EANPage
	maxEANPageImages     = 50  // max images to store in EANPage
)

type EANPageRepo struct {
	Store            *Store
	CategoryRepo     *CategoryRepo
	CatalogizeNew    bool                             // if true, auto-catalogize new EAN pages
	Catalogizer      interface{}                      // *catalogizer.Catalogizer (interface to avoid import cycle)
	catalogizerCache []tokenizer.CachedCategoryTokens // pre-loaded for batch operations
	txn              *makodb.Transaction              // active transaction (nil if not in transaction)
}

func NewEANPageRepo(store *Store) *EANPageRepo {
	return &EANPageRepo{Store: store}
}

// SetCategoryRepo attaches a CategoryRepo for auto-catalogization.
func (r *EANPageRepo) SetCategoryRepo(cr *CategoryRepo) {
	r.CategoryRepo = cr
}

// EnableCatalogizeNew enables auto-catalogization for new EAN pages.
func (r *EANPageRepo) EnableCatalogizeNew(enabled bool) {
	r.CatalogizeNew = enabled
}

// extractKeywordsFromProduct extracts keywords from product name and shop category.
// Returns a space-separated string of individual words (no phrases, no prepositions)
// for catalogization. The catalogizer will combine them later.
func extractKeywordsFromProduct(p *model.Product) string {
	if p == nil {
		return ""
	}

	// Tokenize product name into individual words
	nameTokens := tokenizer.Tokenize(p.Name)
	keywords := make([]string, 0, len(nameTokens))
	for _, t := range nameTokens {
		keywords = append(keywords, t.Word)
	}

	// Also tokenize shop category if present (split by ">" first)
	if p.ShopCategory != "" {
		parts := strings.Split(p.ShopCategory, ">")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			catTokens := tokenizer.Tokenize(part)
			for _, t := range catTokens {
				keywords = append(keywords, t.Word)
			}
		}
	}

	return strings.Join(keywords, " ")
}

// SetTransaction sets the active transaction for this repo.
// When set, all operations will use the transaction.
func (r *EANPageRepo) SetTransaction(txn *makodb.Transaction) {
	r.txn = txn
}

// ClearTransaction clears the active transaction.
func (r *EANPageRepo) ClearTransaction() {
	r.txn = nil
}

// LoadCatalogizerCache loads category token indexes for fast batch auto-catalogization.
// Call this before BatchUpsertFromProducts to avoid repeated TurboRawRead calls.
// Also rebuilds category tokens if they don't exist.
func (r *EANPageRepo) LoadCatalogizerCache() error {
	if r.Store == nil {
		return nil
	}

	// Rebuild category tokens if the catalogizer is available
	if r.Catalogizer != nil {
		fmt.Printf("[EANPAGE] Catalogizer is set, attempting type assertion...\n")
		if catz, ok := r.Catalogizer.(interface {
			RebuildAllCategoryTokens() error
		}); ok {
			fmt.Printf("[EANPAGE] Type assertion succeeded, rebuilding category tokens...\n")
			if err := catz.RebuildAllCategoryTokens(); err != nil {
				fmt.Printf("[EANPAGE] WARN: rebuild category tokens: %v\n", err)
			}
		} else {
			fmt.Printf("[EANPAGE] Type assertion failed for RebuildAllCategoryTokens\n")
		}
	} else {
		fmt.Printf("[EANPAGE] Catalogizer is not set\n")
	}

	categories, err := r.CategoryRepo.ListAll()
	if err != nil {
		return fmt.Errorf("list categories for cache: %w", err)
	}

	const turboKeyCatTokens = "cat_tokens:"
	var cached []tokenizer.CachedCategoryTokens
	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		data, err := r.Store.DB().TurboRawRead(turboKeyCatTokens + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(data) == 0 {
			continue
		}

		tokens, err := r.Store.DB().TurboGetIndexTokens(turboKeyCatTokens + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(tokens) == 0 {
			continue
		}

		//cached = append(cached, tokenizer.CachedCategoryTokens{
		//	ID:       cat.ID,
		//	IsActive: true,
		//	Tokens:   tokens,
		//})
	}

	r.catalogizerCache = cached
	fmt.Printf("[EANPAGE] Loaded catalogizer cache: %d categories with tokens\n", len(cached))
	return nil
}

// CreateNoListIndex creates a new EAN page WITHOUT adding to eanpage_list index.
// Used in batch operations where list index is updated once after all creations.
func (r *EANPageRepo) CreateNoListIndex(s *model.EANPage) error {
	if s.EAN == "" {
		return fmt.Errorf("ean is required")
	}

	id, err := r.Store.NextID("eanpage")
	if err != nil {
		return fmt.Errorf("next_id eanpage: %w", err)
	}
	s.ID = id
	s.CreatedAt = time.Now().Unix()
	s.UpdatedAt = time.Now().Unix()
	if s.Slug == "" {
		s.Slug = toEANPageSlug(s.EAN, s.Title)
	}

	data := MarshalEANPage(*s)
	if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
		return fmt.Errorf("save eanpage: %w", err)
	}

	// Turbo index: eanpage_ean:<ean>
	eanKey := turboKeyEANPageEAN + s.EAN
	if err := r.Store.TurboWrite(eanKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_ = r.Store.DocDelete(KeyEANPage(s.ID))
		return fmt.Errorf("turbo index eanpage_ean: %w", err)
	}

	// Turbo index: eanpage_slug:<slug>
	slugKey := turboKeyEANPageSlug + s.Slug
	if err := r.Store.TurboWrite(slugKey, []byte(KeyEANPage(id))); err != nil {
		_ = r.Store.TurboWrite(eanKey, []byte{})
		_ = r.Store.DocDelete(KeyEANPage(s.ID))
		return fmt.Errorf("turbo index eanpage_slug: %w", err)
	}

	return nil
}

// Create creates a new EAN page.
func (r *EANPageRepo) Create(s *model.EANPage) error {
	if s.EAN == "" {
		return fmt.Errorf("ean is required")
	}

	id, err := r.Store.NextID("eanpage")
	if err != nil {
		return fmt.Errorf("next_id eanpage: %w", err)
	}
	s.ID = id
	s.CreatedAt = time.Now().Unix()
	s.UpdatedAt = time.Now().Unix()
	if s.Slug == "" {
		s.Slug = toEANPageSlug(s.EAN, s.Title)
	}

	data := MarshalEANPage(*s)
	if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
		return fmt.Errorf("save eanpage: %w", err)
	}

	// Turbo index: eanpage_list
	if _, err := r.Store.db.TurboPutIndexString(TurboKeyEANPageList, KeyEANPage(id)); err != nil {
		_ = r.Store.DocDelete(KeyEANPage(s.ID))
		return fmt.Errorf("turbo index eanpage_list: %w", err)
	}

	// Turbo index: eanpage_ean:<ean>
	eanKey := turboKeyEANPageEAN + s.EAN
	if err := r.Store.TurboWrite(eanKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndexString(TurboKeyEANPageList, KeyEANPage(id))
		_ = r.Store.DocDelete(KeyEANPage(s.ID))
		return fmt.Errorf("turbo index eanpage_ean: %w", err)
	}

	// Turbo index: eanpage_slug:<slug>
	slugKey := turboKeyEANPageSlug + s.Slug
	if err := r.Store.TurboWrite(slugKey, []byte(KeyEANPage(id))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndexString(TurboKeyEANPageList, KeyEANPage(id))
		_ = r.Store.TurboWrite(eanKey, []byte{})
		_ = r.Store.DocDelete(KeyEANPage(s.ID))
		return fmt.Errorf("turbo index eanpage_slug: %w", err)
	}

	return nil
}

// Get returns a EAN page by ID.
func (r *EANPageRepo) Get(id int64) (*model.EANPage, error) {
	data, err := r.Store.DocGet(KeyEANPage(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("ean page %d not found", id)
		}
		return nil, fmt.Errorf("get eanpage %d: %w", id, err)
	}
	return UnmarshalEANPage(data)
}

// GetBySCU returns a EAN page by EAN.
func (r *EANPageRepo) GetByEAN(ean string) (*model.EANPage, error) {
	if ean == "" {
		return nil, fmt.Errorf("ean is empty")
	}
	eanKey := turboKeyEANPageEAN + ean
	data, err := r.Store.db.TurboRawRead(eanKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("ean page with ean %q not found", ean)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetBySlug returns a EAN page by slug.
func (r *EANPageRepo) GetBySlug(slug string) (*model.EANPage, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is empty")
	}
	slugKey := turboKeyEANPageSlug + slug
	data, err := r.Store.db.TurboRawRead(slugKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("ean page with slug %q not found", slug)
	}
	// Data is stored as KeyEANPage(id), e.g. "eanpage:73"
	// Extract the ID from the key
	key := string(data)
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid slug index format: %q", key)
	}
	var id int64
	if _, err := fmt.Sscanf(parts[1], "%d", &id); err != nil {
		return nil, fmt.Errorf("invalid slug index ID: %q", parts[1])
	}
	return r.Get(id)
}

// Update updates a EAN page.
func (r *EANPageRepo) Update(id int64, updater func(*model.EANPage)) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSCU := s.EAN
	oldSlug := s.Slug
	updater(s)
	s.UpdatedAt = time.Now().Unix()

	data := MarshalEANPage(*s)
	if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
		return fmt.Errorf("update eanpage: %w", err)
	}

	// Update EAN index if changed
	if oldSCU != s.EAN {
		_ = r.Store.TurboWrite(turboKeyEANPageEAN+oldSCU, []byte{})
		if s.EAN != "" {
			if err := r.Store.TurboWrite(turboKeyEANPageEAN+s.EAN, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update eanpage_scu index: %w", err)
			}
		}
	}

	// Update slug index if changed
	if oldSlug != s.Slug {
		_ = r.Store.TurboWrite(turboKeyEANPageSlug+oldSlug, []byte{})
		if s.Slug != "" {
			if err := r.Store.TurboWrite(turboKeyEANPageSlug+s.Slug, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update eanpage_slug index: %w", err)
			}
		}
	}

	return nil
}

// UpdateLikeDislikeCount updates the like_count and dislike_count for an eanpage.
func (r *EANPageRepo) UpdateLikeDislikeCount(id int64, likeCount, dislikeCount int) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}

	s.LikeCount = likeCount
	s.DislikeCount = dislikeCount
	s.UpdatedAt = time.Now().Unix()

	data := MarshalEANPage(*s)
	if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
		return fmt.Errorf("update eanpage like/dislike count: %w", err)
	}

	return nil
}

// List returns all EAN pages via turbo index.
func (r *EANPageRepo) List() ([]model.EANPage, error) {
	tokens, err := r.Store.db.TurboGetIndexTokens(TurboKeyEANPageList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	docs, err := r.Store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get eanpages: %w", err)
	}

	var result []model.EANPage
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		s, err := UnmarshalEANPage(doc)
		if err != nil {
			continue
		}
		result = append(result, *s)
	}
	return result, nil
}

// ListAll returns all EAN pages (alias for List).
func (r *EANPageRepo) ListAll() ([]model.EANPage, error) {
	return r.List()
}

// Delete removes a EAN page.
func (r *EANPageRepo) Delete(id int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove from the ID registry bucket.
	if err := UnregisterEANPageID(nil, r.Store, id); err != nil {
		fmt.Printf("WARN: unregister eanpage id %d: %v\n", id, err)
	}

	// Remove turbo indexes
	_, _ = r.Store.db.TurboDeleteIndexString(TurboKeyEANPageList, KeyEANPage(id))
	if s.EAN != "" {
		_ = r.Store.TurboWrite(turboKeyEANPageEAN+s.EAN, []byte{})
	}
	if s.Slug != "" {
		_ = r.Store.TurboWrite(turboKeyEANPageSlug+s.Slug, []byte{})
	}

	if err := r.Store.DocDelete(KeyEANPage(id)); err != nil {
		return fmt.Errorf("delete eanpage: %w", err)
	}
	return nil
}

// AddProduct increments product count for this EAN page.
// NOTE: Product→EAN link is stored in Product.EAN field.
// EAN→Products query via turbo index "ean:{ean}" in TurboProductSearch.
func (r *EANPageRepo) AddProduct(id int64, productID int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}
	s.ProductCount++
	s.UpdatedAt = time.Now().Unix()
	data := MarshalEANPage(*s)
	return r.Store.DocPut(KeyEANPage(s.ID), data)
}

// RemoveProduct decrements product count for this EAN page.
// NOTE: Product→EAN link is stored in Product.EAN field.
// EAN→Products query via turbo index "ean:{ean}" in TurboProductSearch.
func (r *EANPageRepo) RemoveProduct(id int64, productID int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}
	if s.ProductCount > 0 {
		s.ProductCount--
	}
	s.UpdatedAt = time.Now().Unix()
	data := MarshalEANPage(*s)
	return r.Store.DocPut(KeyEANPage(s.ID), data)
}

// UpsertBySCU creates or updates a EAN page by EAN.
// If exists: updates fields if new values are provided.
// If not: creates new.
func (r *EANPageRepo) UpsertByEAN(ean string, updater func(*model.EANPage)) (*model.EANPage, error) {
	if ean == "" {
		return nil, fmt.Errorf("ean is required")
	}

	// Try to get existing
	s, err := r.GetByEAN(ean)
	if err == nil {
		// Update existing
		updater(s)
		s.UpdatedAt = time.Now().Unix()
		data := MarshalEANPage(*s)
		if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
			return nil, fmt.Errorf("update eanpage: %w", err)
		}
		return s, nil
	}

	// Create new
	s = &model.EANPage{
		EAN:      ean,
		IsActive: true,
	}
	updater(s)
	if s.Slug == "" {
		s.Slug = toEANPageSlug(s.EAN, s.Title)
	}
	if s.SeoURL == "" {
		s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, nil)
	}
	if err := r.Create(s); err != nil {
		return nil, err
	}
	return s, nil
}

// UpsertFromProduct creates or updates a EAN page from a product.
// This is the main entry point for import-time indexing.
// Merges attributes, images, and updates min_price.
func (r *EANPageRepo) UpsertFromProduct(product *model.Product) error {
	if product.EAN == "" {
		return nil
	}

	// Try to get existing EAN page
	s, err := r.GetByEAN(product.EAN)
	if err == nil {
		// Update existing: merge data
		return r.updateEANPageFromProduct(s, product)
	}

	// Create new EAN page from product
	// Category is determined ONLY by catalogizer (anchor keywords), ignoring product.CategoryID.
	categoryID := int64(0)
	if r.CatalogizeNew && r.CategoryRepo != nil {
		if catID, err := r.autoCatalogize(product); err == nil && catID > 0 {
			categoryID = catID
		}
	}

	slug := toEANPageSlug(product.EAN, product.Name)
	s = &model.EANPage{
		EAN:          product.EAN,
		Slug:         slug,
		Title:        parseTitleFromProductName(product.Name),
		Description:  product.Description,
		Content:      product.Description,
		Images:       limitStrings(deduplicateStrings(product.Images), maxEANPageImages),
		CategoryID:   categoryID,
		Brand:        product.Brand,
		BrandID:      product.BrandID,
		IsActive:     true,
		MinPrice:     product.Price,
		Currency:     product.Currency,
		Attributes:   mergeAttributes(nil, product.Attributes),
		ProductCount: 1,
		SeoURL:       r.ComputeSeoURL(slug, categoryID, nil),
		CreatedAt:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}

	return r.Create(s)
}

// updateEANPageFromProduct merges product data into an existing EAN page.
func (r *EANPageRepo) updateEANPageFromProduct(s *model.EANPage, product *model.Product) error {
	// Increment product count (link is in Product.EAN, not stored here)
	s.ProductCount++

	// Update min_price
	if product.Price < s.MinPrice || s.MinPrice == 0 {
		s.MinPrice = product.Price
	}

	// Merge images (unique, limited)
	s.Images = limitStrings(mergeUniqueStrings(s.Images, product.Images), maxEANPageImages)

	// Merge attributes (no duplicates)
	s.Attributes = mergeAttributes(s.Attributes, product.Attributes)

	// Update description if empty
	if s.Description == "" && product.Description != "" {
		s.Description = product.Description
		s.Content = product.Description
	}

	// Update category if not set and catalogizer is enabled
	if s.CategoryID == 0 && r.CatalogizeNew && r.CategoryRepo != nil {
		if catID, err := r.autoCatalogize(product); err == nil && catID > 0 {
			s.CategoryID = catID
		}
	}

	// Ensure SeoURL is set
	if s.SeoURL == "" {
		s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, nil)
	}

	s.UpdatedAt = time.Now().Unix()

	// Save
	data := MarshalEANPage(*s)
	return r.Store.DocPut(KeyEANPage(s.ID), data)
}

// LinkProductBySCU finds or creates a EAN page for the given EAN and links the product.
// This is the main entry point for import-time linking.
func (r *EANPageRepo) LinkProductByEAN(ean string, product *model.Product) error {
	if ean == "" {
		return nil
	}

	// Try to find existing EAN page
	s, err := r.GetByEAN(ean)
	if err == nil {
		// Exists — just link product
		return r.AddProduct(s.ID, product.ID)
	}

	// Create new EAN page from product data
	s, err = r.UpsertByEAN(ean, func(s *model.EANPage) {
		s.Title = product.Name
		s.Description = product.Description
		s.Content = product.Description
		s.Images = product.Images
		s.CategoryID = product.CategoryID
		s.Brand = product.Brand
		s.BrandID = product.BrandID
		s.MinPrice = product.Price
		s.Currency = product.Currency
		s.IsActive = true
	})
	if err != nil {
		return err
	}

	// Link product
	return r.AddProduct(s.ID, product.ID)
}

// BatchLinkProductsByEAN links multiple products to EAN pages in batch.
// eanToProducts is a map of EAN to products.
func (r *EANPageRepo) BatchLinkProductsByEAN(eanToProducts map[string][]*model.Product) error {
	if len(eanToProducts) == 0 {
		return nil
	}

	// First pass: check which EAN pages already exist
	existingPages := make(map[string]*model.EANPage) // ean -> page
	var toCreate []string

	for ean := range eanToProducts {
		if ean == "" {
			continue
		}

		s, err := r.GetByEAN(ean)
		if err == nil {
			existingPages[ean] = s
		} else {
			toCreate = append(toCreate, ean)
		}
	}

	// Second pass: create new EAN pages (without list index)
	var newEANPageKeys []string
	for _, ean := range toCreate {
		prods := eanToProducts[ean]
		if len(prods) == 0 {
			continue
		}

		// Use first product's data
		p := prods[0]
		slug := toEANPageSlug(ean, p.Name)
		s := &model.EANPage{
			EAN:          ean,
			Slug:         slug,
			Title:        p.Name,
			Content:      p.Description,
			Images:       limitStrings(deduplicateStrings(p.Images), maxEANPageImages),
			CategoryID:   p.CategoryID,
			Brand:        p.Brand,
			BrandID:      p.BrandID,
			MinPrice:     p.Price,
			Currency:     p.Currency,
			IsActive:     true,
			ProductCount: len(prods),
		}
		s.SeoURL = r.ComputeSeoURL(slug, p.CategoryID, nil)

		if err := r.CreateNoListIndex(s); err != nil {
			fmt.Printf("WARN: batch create eanpage for EAN %s: %v\n", ean, err)
			continue
		}

		existingPages[ean] = s
		newEANPageKeys = append(newEANPageKeys, KeyEANPage(s.ID))
	}

	// Third pass: batch add all new EAN pages to list index (single write)
	if len(newEANPageKeys) > 0 {
		if _, err := r.Store.db.TurboPutBatchIndexString(TurboKeyEANPageList, newEANPageKeys); err != nil {
			fmt.Printf("WARN: batch add to eanpage_list: %v\n", err)
		}
	}

	// NOTE: No need to link products to EAN pages explicitly.
	// The link is stored in the turbo index "ean:{ean}" which is already created.

	return nil
}

// autoCatalogize is the single-product variant of auto-catalogization: it
// builds the category token sets on the fly (fine for one page, use the
// batched catalogTokenSets + AutoCatalogizeWithSets for bulk imports).
func (r *EANPageRepo) autoCatalogize(p *model.Product) (int64, error) {
	return AutoCatalogizeWithSets(p, r.catalogTokenSets()), nil
}

// catalogTokenSets loads the catalogizer token sets for all active categories
// ONCE (cat_tokens:{catID} -> set of token hashes). Building this once per
// batch turns auto-catalogization from O(pages x categories) index reads into
// in-memory scoring.
func (r *EANPageRepo) catalogTokenSets() map[int64]map[uint64]struct{} {
	if r.CategoryRepo == nil || r.Store == nil {
		return nil
	}
	categories, err := r.CategoryRepo.ListAll()
	if err != nil {
		return nil
	}
	sets := make(map[int64]map[uint64]struct{}, len(categories))
	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}
		data, err := r.Store.DB().TurboRawRead(turboKeyCatTokens + strconv.FormatInt(cat.ID, 10))
		if err != nil || len(data) == 0 {
			continue
		}
		kt := makodb.TurboUnsafeReadTokens(data)
		set := make(map[uint64]struct{}, len(kt))
		for _, t := range kt {
			set[t[0]] = struct{}{}
		}
		if len(set) > 0 {
			sets[cat.ID] = set
		}
	}
	return sets
}

// CatalogTokenSets is the exported batch helper: loads the catalogizer token
// sets for all active categories once (for bulk scoring in the API layer).
func (r *EANPageRepo) CatalogTokenSets() map[int64]map[uint64]struct{} {
	return r.catalogTokenSets()
}

// BestCategoryByText scores free text (page keywords/title) against the
// prebuilt category token sets and returns the best category ID (0 = no
// match). Same scoring as the catalogizer's per-product matching.
func BestCategoryByText(text string, sets map[int64]map[uint64]struct{}) int64 {
	if len(sets) == 0 || strings.TrimSpace(text) == "" {
		return 0
	}
	tokens := tokenizer.Tokenize(text)
	if len(tokens) == 0 {
		return 0
	}
	var bestCatID int64
	bestScore := 0
	for catID, set := range sets {
		score := 0
		for _, t := range tokens {
			if _, ok := set[t.Hash]; ok {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestCatID = catID
		}
	}
	return bestCatID
}

// AutoCatalogizeWithSets determines the best category for a product by token
// overlap with the prebuilt category token sets (see catalogTokenSets).
// Returns 0 when nothing matches.
func AutoCatalogizeWithSets(p *model.Product, sets map[int64]map[uint64]struct{}) int64 {
	if p == nil {
		return 0
	}

	// Tokenize product name (same input the catalogizer trains on) plus the
	// last segment of the shop category, if present.
	text := p.Name
	if p.ShopCategory != "" {
		parts := strings.Split(p.ShopCategory, "\\")
		last := strings.TrimSpace(parts[len(parts)-1])
		if last != "" {
			text = text + " " + last
		}
	}
	return BestCategoryByText(text, sets)
}

// --- helpers ---

// EANPageKeyForProduct returns the page key for a product: its EAN, or a
// stable name-based key when EAN is absent (e.g. suppliers without barcodes).
func EANPageKeyForProduct(p *model.Product) string {
	if p.EAN != "" {
		return p.EAN
	}
	// Name-based fallback key.
	return "nm:" + strings.ToLower(strings.Join(strings.Fields(p.Name), " "))
}

// ProductEANIndexKey returns the key under which a product must be indexed in
// the "ean:" turbo index so GetProductsByEAN finds it on its EAN page.
// For products with an EAN that's the EAN; for name-based pages it's the same
// "nm:<name>" fallback as EANPageKeyForProduct (so pages and products always
// match). Returns "" when neither is available.
func ProductEANIndexKey(p *model.Product) string {
	if p.EAN != "" {
		return p.EAN
	}
	pk := EANPageKeyForProduct(p)
	// Guard against empty names ("nm:" with nothing after it).
	if pk == "" || pk == "nm:" {
		return ""
	}
	return pk
}

// toEANPageSlug creates a URL-friendly slug from EAN and title.
func toEANPageSlug(ean, title string) string {
	// Prefer title if available, otherwise use EAN
	base := title
	if base == "" {
		base = ean
	}
	return slug.SlugKeepCase(base)
}

// parseTitleFromProductName extracts a clean title from product name.
// E.g. "Samsung Galaxy S7 Черный" → "Samsung Galaxy S7"
// Removes color/size/option suffixes.
func parseTitleFromProductName(name string) string {
	if name == "" {
		return name
	}

	// Common option keywords to strip (case-insensitive)
	optionPatterns := []string{
		" черный", " белый", " красный", " синий", " зелёный", " зеленый",
		" серый", " розовый", " золотой", " серебристый", " коричневый",
		" матовый", " глянцевый", " без чехла", " с чехлом",
		" 64 гб", " 128 гб", " 256 гб", " 512 гб", " 1 тб",
		" 64gb", " 128gb", " 256gb", " 512gb", " 1tb",
		" 4 гб", " 8 гб", " 16 гб", " 32 гб",
		" 4gb", " 8gb", " 16gb", " 32gb",
	}

	result := name
	for _, pattern := range optionPatterns {
		lowerResult := strings.ToLower(result)
		idx := strings.Index(lowerResult, pattern)
		if idx >= 0 {
			result = strings.TrimSpace(result[:idx])
		}
	}

	if len(result) < 5 {
		return name
	}

	return result
}

// limitStrings truncates a slice to at most maxLen elements.
func limitStrings(s []string, maxLen int) []string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// mergeUniqueStrings merges two string slices, keeping only unique values.
func mergeUniqueStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))

	for _, s := range a {
		if s != "" {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				result = append(result, s)
			}
		}
	}
	for _, s := range b {
		if s != "" {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				result = append(result, s)
			}
		}
	}

	return result
}

// deduplicateStrings removes duplicate strings from a slice.
func deduplicateStrings(slice []string) []string {
	seen := make(map[string]struct{}, len(slice))
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != "" {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				result = append(result, s)
			}
		}
	}
	return result
}

// mergeAttributes merges two attribute maps.
// For duplicates: keeps first occurrence (no overwriting).
// mergeAttributes merges two KeyValue slices. Keys from 'a' take precedence.
// Allows multiple values per Key (deduplicates by Key+Value pair).
// Values longer than 40 runes are excluded.
func mergeAttributes(a, b []model.KeyValue) []model.KeyValue {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		return filterAttrValues(b)
	}
	if len(b) == 0 {
		return filterAttrValues(a)
	}

	// Start with a copy of a
	result := cloneKeyValueSlice(a)
	// Add entries from b that don't exist in a (check both Key and Value)
	for _, kv := range b {
		if model.IsAttrValueTooLong(kv.Value) {
			continue
		}
		found := false
		for _, r := range result {
			if r.Key == kv.Key && r.Value == kv.Value {
				found = true
				break
			}
		}
		if !found {
			result = append(result, kv)
		}
	}
	// Final pass: remove any remaining long values from result
	return filterAttrValues(result)
}

// MergeAttributes is the exported version of mergeAttributes.
// Union by Key+Value: entries from 'a' take precedence, entries from 'b'
// supplement 'a' without overwriting existing pairs.
func MergeAttributes(a, b []model.KeyValue) []model.KeyValue {
	return mergeAttributes(a, b)
}

// cloneKeyValueSlice creates a copy of a KeyValue slice.
func cloneKeyValueSlice(src []model.KeyValue) []model.KeyValue {
	if src == nil {
		return nil
	}
	result := make([]model.KeyValue, len(src))
	copy(result, src)
	return result
}

// ComputeSeoURL builds seo_url for a EAN page: "/shop/{treePath}/{slug}"
// Uses treePathCache to avoid repeated DB calls. treePathCache may be nil
// (single-item callers); in that case the path is computed without caching.
func (r *EANPageRepo) ComputeSeoURL(slug string, categoryID int64, treePathCache map[int64][]string) string {
	if categoryID == 0 || r.CategoryRepo == nil {
		return "/shop/" + slug
	}

	if treePathCache != nil {
		if treePath, cached := treePathCache[categoryID]; cached {
			return "/shop/" + strings.Join(treePath, "/") + "/" + slug
		}
	}

	treePath, err := r.CategoryRepo.GetTreePath(categoryID)
	if err != nil || len(treePath) == 0 {
		return "/shop/" + slug
	}
	if treePathCache != nil {
		treePathCache[categoryID] = treePath
	}

	return "/shop/" + strings.Join(treePath, "/") + "/" + slug
}

// CatalogizeResult holds the result of matching a product to a category.
type CatalogizeResult struct {
	ProductID       int64
	OldCategoryID   int64
	NewCategoryID   int64
	NewCategorySlug string
	Score           int
	MatchedTokens   []string // matched token words (stems)
}

const turboKeyCatTokens = "cat_tokens:"

// MatchProductToCategories returns ALL matching categories sorted by score (descending).
// Used for the test tool in admin panel.
func (r *EANPageRepo) MatchProductToCategories(name string) []CatalogizeResult {
	tokens := tokenizer.Tokenize(name)
	if len(tokens) == 0 {
		return nil
	}

	hashes := make([]uint64, len(tokens))
	words := make(map[uint64]string)
	for i, t := range tokens {
		hashes[i] = t.Hash
		words[t.Hash] = t.Word
	}

	categories, err := r.CategoryRepo.ListAll()
	if err != nil {
		return nil
	}

	var results []CatalogizeResult

	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		data, err := r.Store.DB().TurboRawRead(turboKeyCatTokens + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(data) == 0 {
			continue
		}

		catTokensKey128 := makodb.TurboUnsafeReadTokens(data)
		// Convert key128 to uint64
		catTokens := make([]uint64, len(catTokensKey128))
		for i, t := range catTokensKey128 {
			catTokens[i] = t[0]
		}

		overlap := tokenizer.CountTokenOverlap(hashes, catTokens)

		if overlap <= 0 {
			continue
		}

		var matched []string
		catSet := make(map[uint64]bool)
		for _, h := range catTokens {
			catSet[h] = true
		}
		for _, h := range hashes {
			if catSet[h] && words[h] != "" {
				matched = append(matched, words[h])
			}
		}

		results = append(results, CatalogizeResult{
			NewCategoryID:   cat.ID,
			NewCategorySlug: cat.Slug,
			Score:           overlap,
			MatchedTokens:   matched,
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// CountEANPagesWithAttrCode counts how many EAN pages have the given attribute code.
// Uses eanpage_attr_code:{code} turbo index for O(1) lookup.
func (r *EANPageRepo) CountEANPagesWithAttrCode(code string) int {
	if r.Store == nil {
		return 0
	}
	key := "eanpage_attr_code:" + code
	tokens, err := r.Store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return 0
	}
	return len(tokens)
}

// This is much faster than recalculating all pages when only a subset was affected by import.

// RecalculateCountsAndMinPricesForPages recalculates ProductCount and MinPrice
// for the given EAN pages in a SINGLE pass. Product counts come from the
// per-EAN index documents (ean_products:{key} -> product IDs), prices from the
// prebuilt productID -> price map (the company price documents) — no product
// documents are read on the hot path. Only pages whose count or min price
// actually changed are rewritten.
//
// prices may be nil or incomplete — in that case min prices are left
// unchanged (counts are still recalculated). Pages without an EAN index
// document are backfilled once from the ean:{key} index + product documents
// (migration path for data imported before the index existed).
func (r *EANPageRepo) RecalculateCountsAndMinPricesForPages(pageIDs []int64, prices map[int64]float64) error {
	if r.Store == nil || len(pageIDs) == 0 {
		return nil
	}

	start := time.Now()
	updated := 0

	const batchSize = 50000
	for start0 := 0; start0 < len(pageIDs); start0 += batchSize {
		end := start0 + batchSize
		if end > len(pageIDs) {
			end = len(pageIDs)
		}
		batch := pageIDs[start0:end]

		keys := make([]any, len(batch))
		for i, id := range batch {
			keys[i] = KeyEANPage(id)
		}
		docs, err := r.Store.db.MultiGetByDocIDs(keys)
		if err != nil {
			return fmt.Errorf("multi get eanpages: %w", err)
		}

		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			sp, err := UnmarshalEANPage(doc)
			if err != nil || sp.EAN == "" {
				continue
			}

			ids := r.eanProductIDs(sp.EAN)

			// Min price from the merged company price documents.
			var minPrice float64
			found := false
			for _, id := range ids {
				if price, ok := prices[id]; ok {
					if !found || price < minPrice {
						minPrice = price
						found = true
					}
				}
			}

			changed := false
			if len(ids) != sp.ProductCount {
				sp.ProductCount = len(ids)
				changed = true
			}
			if found && minPrice != sp.MinPrice {
				sp.MinPrice = minPrice
				changed = true
			}
			if !changed {
				continue
			}
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(*sp)
			if err := r.Store.DocPut(KeyEANPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update counts/min_price for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}
	}

	fmt.Printf("[EANPAGE] RecalculateCountsAndMinPricesForPages: %d pages, updated %d, %v\n",
		len(pageIDs), updated, time.Since(start))
	return nil
}

// eanProductIDs returns the product IDs sharing a page key. Fast path: the
// EAN index document. When the document is missing (data imported before the
// index existed), it is backfilled from the ean:{key} index + product
// documents and persisted for subsequent runs.
func (r *EANPageRepo) eanProductIDs(pageKey string) []int64 {
	doc, err := LoadEANIndexDoc(nil, r.Store, pageKey)
	if err == nil && doc != nil {
		return doc.ProductIDs
	}

	// Backfill: resolve product IDs via documents (never via raw tokens).
	tokens, err := r.Store.db.TurboGetIndexTokens("ean:" + pageKey)
	if err != nil || len(tokens) == 0 {
		return nil
	}
	docs, err := r.Store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(docs))
	for _, d := range docs {
		if len(d) == 0 {
			continue
		}
		p, err := UnmarshalProduct(d)
		if err != nil {
			continue
		}
		ids = append(ids, p.ID)
	}
	if err := SaveEANIndexDoc(nil, r.Store, pageKey, &EANIndexDoc{ProductIDs: ids}); err != nil {
		fmt.Printf("WARN: backfill ean index %q: %v\n", pageKey, err)
	}
	return ids
}

// AllEANPageIDs returns the IDs of all EAN pages from the ID registry
// documents (small eanpage_ids:{bucket} docs maintained on create/delete).
// When the registry is empty — a database whose pages predate it — the IDs
// are backfilled once via index pagination and persisted.
func (r *EANPageRepo) AllEANPageIDs() ([]int64, error) {
	if r.Store == nil {
		return nil, nil
	}
	if ids := LoadEANPageIDsFromRegistry(r.Store); len(ids) > 0 {
		return ids, nil
	}

	// Backfill: paginate the eanpage_list index, collect IDs, persist.
	ids := make([]int64, 0, 1024)
	err := r.ForEachEANPageBatch(50000, func(pages []model.EANPage) error {
		for i := range pages {
			ids = append(ids, pages[i].ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		if err := SaveEANPageIDsToRegistry(r.Store, ids); err != nil {
			fmt.Printf("WARN: backfill eanpage id registry: %v\n", err)
		}
	}
	return ids, nil
}

// ForEachEANPageBatch paginates the eanpage_list index directly in the
// engine (token + offset + limit) and hands each batch of fully loaded pages
// to the callback. Empty/failed docs are skipped.
func (r *EANPageRepo) ForEachEANPageBatch(batchSize int, cb func(pages []model.EANPage) error) error {
	if r.Store == nil {
		return nil
	}
	const maxBatch = 50000
	if batchSize <= 0 || batchSize > maxBatch {
		batchSize = maxBatch
	}
	for offset := 0; ; offset += batchSize {
		tokens, err := r.Store.db.TurboGetIndexTokensFiltered(TurboKeyEANPageList, nil, nil, false, batchSize, offset)
		if err != nil {
			return fmt.Errorf("turbo index tokens: %w", err)
		}
		if len(tokens) == 0 {
			return nil
		}
		docs, err := r.Store.db.MultiGetByDocIDs(tokens)
		if err != nil {
			return fmt.Errorf("multi get eanpages: %w", err)
		}
		pages := make([]model.EANPage, 0, len(docs))
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			sp, err := UnmarshalEANPage(doc)
			if err != nil {
				continue
			}
			pages = append(pages, *sp)
		}
		if err := cb(pages); err != nil {
			return err
		}
		if len(tokens) < batchSize {
			return nil
		}
	}
}

// RecalculateMinPricesForPages was removed: use the combined
// RecalculateCountsAndMinPricesForPages (one pass, price-map based).

// BatchUpsertFromProductsTx is the transactional version of BatchUpsertFromProducts.
// It buffers all writes in the transaction instead of writing directly to the DB.
// BatchUpsertFromProductsTx groups products into EAN pages and creates or
// updates them inside the transaction. Pages with no category (from the
// source data) go through autoCatalogize. deliverySlugs are the importing
// company's delivery method slugs (read ONCE from company settings by the
// caller) — they are stamped as the delivery_method attribute on every
// affected page and indexed by the standard attribute path.
// Returns the productID -> pageID map and the affected page objects in their
// final in-memory state (post merge and catalogization) — use them for
// indexing inside the same transaction instead of re-reading committed state.
func (r *EANPageRepo) BatchUpsertFromProductsTx(txn *Transaction, products []*model.Product, deliverySlugs []string) (map[int64]int64, []*model.EANPage) {
	if len(products) == 0 {
		return nil, nil
	}

	// Collect all page keys and unique category IDs
	pageKeySet := make(map[string]struct{})
	catIDs := make(map[int64]struct{})
	for _, p := range products {
		if pk := EANPageKeyForProduct(p); pk != "" {
			pageKeySet[pk] = struct{}{}
		}
		if p.CategoryID != 0 {
			catIDs[p.CategoryID] = struct{}{}
		}
	}

	upsertStart := time.Now()

	// Batch load existing pages
	existing := make(map[string]*model.EANPage)
	for pk := range pageKeySet {
		if s, err := r.GetByEAN(pk); err == nil {
			existing[pk] = s
			if s.CategoryID != 0 {
				catIDs[s.CategoryID] = struct{}{}
			}
		}
	}
	fmt.Printf("[EANPAGE] upsert: %d page keys, %d existing, load %v\n",
		len(pageKeySet), len(existing), time.Since(upsertStart))

	// Pre-compute tree paths for all category IDs (single pass)
	treePathCache := make(map[int64][]string)
	for catID := range catIDs {
		if r.CategoryRepo != nil {
			if tp, err := r.CategoryRepo.GetTreePath(catID); err == nil && len(tp) > 0 {
				treePathCache[catID] = tp
			}
		}
	}

	// Group products by EAN and merge into EANPage objects
	newPages := make(map[string]*model.EANPage)
	updatedPages := make(map[string]*model.EANPage)

	for _, p := range products {
		pk := EANPageKeyForProduct(p)
		if pk == "" {
			continue
		}

		if s, ok := existing[pk]; ok {
			updatedPages[pk] = s
		} else {
			if _, ok := newPages[pk]; !ok {
				slug := toEANPageSlug(pk, p.Name)
				newPages[pk] = &model.EANPage{
					EAN:          pk,
					Slug:         slug,
					Title:        parseTitleFromProductName(p.Name),
					Description:  p.Description,
					Content:      p.Description,
					Images:       limitStrings(deduplicateStrings(p.Images), maxEANPageImages),
					CategoryID:   p.CategoryID,
					Brand:        p.Brand,
					BrandID:      p.BrandID,
					IsActive:     true,
					MinPrice:     p.Price,
					Currency:     p.Currency,
					Attributes:   mergeAttributes(nil, p.Attributes),
					ProductCount: 1,
					SeoURL:       r.ComputeSeoURL(slug, p.CategoryID, treePathCache),
					Keywords:     extractKeywordsFromProduct(p),
					CreatedAt:    time.Now().Unix(),
					UpdatedAt:    time.Now().Unix(),
				}
			}
		}
	}

	// Merge updates for existing pages
	for _, p := range products {
		pk := EANPageKeyForProduct(p)
		if pk == "" {
			continue
		}
		if s, ok := updatedPages[pk]; ok {
			s.ProductCount++

			if p.Price < s.MinPrice || s.MinPrice == 0 {
				s.MinPrice = p.Price
			}

			s.Images = limitStrings(mergeUniqueStrings(s.Images, p.Images), maxEANPageImages)
			s.Attributes = mergeAttributes(s.Attributes, p.Attributes)

			if s.Description == "" && p.Description != "" {
				s.Description = p.Description
				s.Content = p.Description
			}

			// Update Keywords (always refresh with latest product info)
			s.Keywords = extractKeywordsFromProduct(p)

			if s.SeoURL == "" || s.CategoryID != p.CategoryID {
				s.CategoryID = p.CategoryID
				s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, treePathCache)
			}

			s.UpdatedAt = time.Now().Unix()
		}
	}

	// Auto-catalogize pages left without a category: use the first product
	// of the group as the matching source (same input the catalogizer uses).
	representative := make(map[string]*model.Product, len(newPages)+len(updatedPages))
	for _, p := range products {
		pk := EANPageKeyForProduct(p)
		if pk == "" {
			continue
		}
		if _, ok := representative[pk]; !ok {
			representative[pk] = p
		}
	}
	uncategorized := make([]*model.EANPage, 0, len(newPages)+len(updatedPages))
	for _, s := range newPages {
		if s.CategoryID == 0 {
			uncategorized = append(uncategorized, s)
		}
	}
	for _, s := range updatedPages {
		if s.CategoryID == 0 {
			uncategorized = append(uncategorized, s)
		}
	}
	if len(uncategorized) > 0 {
		// Load the catalogizer token sets ONCE for the whole batch: scoring a
		// page against in-memory sets instead of re-reading every category's
		// token index per page (this was O(pages × categories) index reads).
		catSets := r.catalogTokenSets()
		catalogized := 0
		catzStart := time.Now()
		for _, s := range uncategorized {
			p := representative[s.EAN]
			if p == nil {
				continue
			}
			catID := AutoCatalogizeWithSets(p, catSets)
			if catID == 0 {
				continue
			}
			s.CategoryID = catID
			s.SeoURL = r.ComputeSeoURL(s.Slug, catID, treePathCache)
			catalogized++
		}
		fmt.Printf("[EANPAGE] auto-catalogized %d / %d uncategorized pages in %v (%d categories)\n",
			catalogized, len(uncategorized), time.Since(catzStart), len(catSets))
	}

	// Stamp the company delivery slugs on every affected page: the
	// delivery_method attribute is company-derived, set once here at import
	// time and indexed like any other attribute.
	if len(deliverySlugs) > 0 {
		slugsSet := make(map[string]struct{}, len(deliverySlugs))
		for _, slug := range deliverySlugs {
			if slug != "" {
				slugsSet[slug] = struct{}{}
			}
		}
		for _, s := range newPages {
			s.Attributes = SetDeliveryMethodAttr(s.Attributes, slugsSet)
		}
		for _, s := range updatedPages {
			s.Attributes = SetDeliveryMethodAttr(s.Attributes, slugsSet)
		}
	}

	// Collect the affected pages in their final in-memory state.
	affected := make([]*model.EANPage, 0, len(newPages)+len(updatedPages))
	for _, s := range newPages {
		affected = append(affected, s)
	}
	for _, s := range updatedPages {
		affected = append(affected, s)
	}

	// Create new pages (buffered in transaction)
	created := make(map[string]int64)
	var newEANPageIDs []string
	for ean, s := range newPages {
		if err := r.CreateNoListIndexTx(txn, s); err != nil {
			fmt.Printf("WARN: create eanpage for EAN %s: %v\n", ean, err)
			continue
		}
		created[ean] = s.ID
		newEANPageIDs = append(newEANPageIDs, KeyEANPage(s.ID))
	}

	// Batch add all new EAN pages to eanpage_list index (buffered in transaction)
	if len(newEANPageIDs) > 0 {
		if _, err := txn.TurboPutBatchIndexString(TurboKeyEANPageList, newEANPageIDs); err != nil {
			fmt.Printf("WARN: batch add to eanpage_list: %v\n", err)
		}
	}

	// Update existing pages (buffered in transaction)
	for ean, s := range updatedPages {
		data := MarshalEANPage(*s)
		if err := txn.DocPut(KeyEANPage(s.ID), data); err != nil {
			fmt.Printf("WARN: update eanpage for EAN %s: %v\n", ean, err)
			continue
		}
		created[ean] = s.ID
	}

	// Build productID -> EANPageID map
	result := make(map[int64]int64)
	for _, p := range products {
		if p.EAN == "" {
			continue
		}
		if id, ok := created[p.EAN]; ok {
			result[p.ID] = id
		}
	}

	fmt.Printf("[EANPAGE] upsert: done in %v (new %d, updated %d)\n",
		time.Since(upsertStart), len(newPages), len(updatedPages))
	return result, affected
}

// CreateNoListIndexTx creates an EAN page without adding to list index (transactional version).
func (r *EANPageRepo) CreateNoListIndexTx(txn *Transaction, s *model.EANPage) error {
	if s.ID == 0 {
		id, err := r.Store.NextID("eanpage")
		if err != nil {
			return fmt.Errorf("next_id eanpage: %w", err)
		}
		s.ID = id
	}

	data := MarshalEANPage(*s)
	if err := txn.DocPut(KeyEANPage(s.ID), data); err != nil {
		return fmt.Errorf("save eanpage: %w", err)
	}

	// Turbo index: eanpage_ean:<ean> (buffered in transaction)
	eanKey := turboKeyEANPageEAN + s.EAN
	if err := txn.TurboWrite(eanKey, []byte(strconv.FormatInt(s.ID, 10))); err != nil {
		return fmt.Errorf("turbo index eanpage_ean: %w", err)
	}

	// Turbo index: eanpage_slug:<slug> (buffered in transaction)
	slugKey := turboKeyEANPageSlug + s.Slug
	if err := txn.TurboWrite(slugKey, []byte(KeyEANPage(s.ID))); err != nil {
		return fmt.Errorf("turbo index eanpage_slug: %w", err)
	}

	// ID registry bucket (buffered in transaction).
	if err := RegisterEANPageID(txn, r.Store, s.ID); err != nil {
		return fmt.Errorf("register eanpage id: %w", err)
	}

	return nil
}
