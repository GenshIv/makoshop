package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/slug"
	"github.com/GenshIv/makoshop/internal/tokenizer"
)

const (
	TurboKeyEANPageList = "eanpage_list"
	turboKeyEANPageEAN  = "eanpage_ean:"  // prefix for SCU lookup
	turboKeyEANPageSlug = "eanpage_slug:" // prefix for slug lookup

	// Limits to prevent SCU pages from growing too large in DB
	maxEANPageProductIDs = 500 // max product IDs to store in EANPage
	maxEANPageImages     = 50  // max images to store in EANPage
)

type EANPageRepo struct {
	Store            *Store
	CategoryRepo     *CategoryRepo
	CatalogizeNew    bool                             // if true, auto-catalogize new SCU pages
	Catalogizer      interface{}                      // *catalogizer.Catalogizer (interface to avoid import cycle)
	catalogizerCache []tokenizer.CachedCategoryTokens // pre-loaded for batch operations
}

func NewEANPageRepo(store *Store) *EANPageRepo {
	return &EANPageRepo{Store: store}
}

// SetCategoryRepo attaches a CategoryRepo for auto-catalogization.
func (r *EANPageRepo) SetCategoryRepo(cr *CategoryRepo) {
	r.CategoryRepo = cr
}

// EnableCatalogizeNew enables auto-catalogization for new SCU pages.
func (r *EANPageRepo) EnableCatalogizeNew(enabled bool) {
	r.CatalogizeNew = enabled
}

// LoadCatalogizerCache loads category token indexes for fast batch auto-catalogization.
// Call this before BatchUpsertFromProducts to avoid repeated TurboRawRead calls.
func (r *EANPageRepo) LoadCatalogizerCache() error {
	if r.Store == nil {
		return nil
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
	fmt.Printf("[SCUPAGE] Loaded catalogizer cache: %d categories with tokens\n", len(cached))
	return nil
}

// CreateNoListIndex creates a new SCU page WITHOUT adding to eanpage_list index.
// Used in batch operations where list index is updated once after all creations.
func (r *EANPageRepo) CreateNoListIndex(s *model.EANPage) error {
	if s.EAN == "" {
		return fmt.Errorf("scu is required")
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

	// Turbo index: eanpage_ean:<scu>
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

// Create creates a new SCU page.
func (r *EANPageRepo) Create(s *model.EANPage) error {
	if s.EAN == "" {
		return fmt.Errorf("scu is required")
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

	// Turbo index: eanpage_ean:<scu>
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

// Get returns a SCU page by ID.
func (r *EANPageRepo) Get(id int64) (*model.EANPage, error) {
	data, err := r.Store.DocGet(KeyEANPage(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("scu page %d not found", id)
		}
		return nil, fmt.Errorf("get eanpage %d: %w", id, err)
	}
	return UnmarshalEANPage(data)
}

// GetBySCU returns a SCU page by SCU.
func (r *EANPageRepo) GetByEAN(scu string) (*model.EANPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is empty")
	}
	eanKey := turboKeyEANPageEAN + scu
	data, err := r.Store.db.TurboRawRead(eanKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("scu page with scu %q not found", scu)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetBySlug returns a SCU page by slug.
func (r *EANPageRepo) GetBySlug(slug string) (*model.EANPage, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is empty")
	}
	slugKey := turboKeyEANPageSlug + slug
	data, err := r.Store.db.TurboRawRead(slugKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("scu page with slug %q not found", slug)
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

// Update updates a SCU page.
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

	// Update SCU index if changed
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

// List returns all SCU pages via turbo index.
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

// ListAll returns all SCU pages (alias for List).
func (r *EANPageRepo) ListAll() ([]model.EANPage, error) {
	return r.List()
}

// Delete removes a SCU page.
func (r *EANPageRepo) Delete(id int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
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

// AddProduct increments product count for this SCU page.
// NOTE: Product→SCU link is stored in Product.EAN field.
// SCU→Products query via turbo index "ean:{scu}" in TurboProductSearch.
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

// RemoveProduct decrements product count for this SCU page.
// NOTE: Product→SCU link is stored in Product.EAN field.
// SCU→Products query via turbo index "ean:{scu}" in TurboProductSearch.
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

// UpsertBySCU creates or updates a SCU page by SCU.
// If exists: updates fields if new values are provided.
// If not: creates new.
func (r *EANPageRepo) UpsertByEAN(scu string, updater func(*model.EANPage)) (*model.EANPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is required")
	}

	// Try to get existing
	s, err := r.GetByEAN(scu)
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
		EAN:      scu,
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

// UpsertFromProduct creates or updates a SCU page from a product.
// This is the main entry point for import-time indexing.
// Merges attributes, images, and updates min_price.
func (r *EANPageRepo) UpsertFromProduct(product *model.Product) error {
	if product.EAN == "" {
		return nil
	}

	// Try to get existing SCU page
	s, err := r.GetByEAN(product.EAN)
	if err == nil {
		// Update existing: merge data
		return r.updateEANPageFromProduct(s, product)
	}

	// Create new SCU page from product
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

// updateEANPageFromProduct merges product data into an existing SCU page.
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

	// Ensure SeoURL is set
	if s.SeoURL == "" {
		s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, nil)
	}

	s.UpdatedAt = time.Now().Unix()

	// Save
	data := MarshalEANPage(*s)
	return r.Store.DocPut(KeyEANPage(s.ID), data)
}

// LinkProductBySCU finds or creates a SCU page for the given SCU and links the product.
// This is the main entry point for import-time linking.
func (r *EANPageRepo) LinkProductByEAN(scu string, product *model.Product) error {
	if scu == "" {
		return nil
	}

	// Try to find existing SCU page
	s, err := r.GetByEAN(scu)
	if err == nil {
		// Exists — just link product
		return r.AddProduct(s.ID, product.ID)
	}

	// Create new SCU page from product data
	s, err = r.UpsertByEAN(scu, func(s *model.EANPage) {
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

// autoCatalogize determines the best category for a product based on anchor keywords.
// Uses pre-built token indexes from catalogizer (cat_tokens:{catID}).
// Returns category ID or 0 if no match found.
func (r *EANPageRepo) autoCatalogize(p *model.Product) (int64, error) {
	if r.CategoryRepo == nil || r.Store == nil {
		return 0, nil
	}

	// Tokenize product name (same as catalogizer)
	productTokens := tokenizer.Tokenize(p.Name)

	// Also tokenize shop_category attribute if present
	for _, attr := range p.Attributes {
		if attr.Key == "shop_category" && attr.Value != "" {
			catTokens := tokenizer.Tokenize(attr.Value)
			productTokens = append(productTokens, catTokens...)
			break
		}
	}

	if len(productTokens) == 0 {
		return 0, nil
	}

	productHashes := make([]uint64, len(productTokens))
	for i, t := range productTokens {
		productHashes[i] = t.Hash
	}

	// Load category IDs from turbo index list or via ListAll
	categories, err := r.CategoryRepo.ListAll()
	if err != nil {
		return 0, err
	}

	var bestCatID int64
	var bestScore int

	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		// Read pre-built tokens from catalogizer index
		tokens, err := r.Store.DB().TurboGetIndexTokens("cat_tokens:" + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(tokens) == 0 {
			continue
		}

		// Count overlap using set
		catSet := make(map[any]bool, len(tokens))
		for _, token := range tokens {
			// Convert Key128 back to uint64 hash (stored as [0, hash])
			catSet[token] = true
		}

		score := 0
		for _, h := range productHashes {
			if catSet[h] {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestCatID = cat.ID
		}
	}

	return bestCatID, nil
}

// --- helpers ---

// eanPageKeyForProduct returns the page key for a product: its EAN, or a
// stable name-based key when EAN is absent (e.g. suppliers without barcodes).
func eanPageKeyForProduct(p *model.Product) string {
	if p.EAN != "" {
		return p.EAN
	}
	// Name-based fallback key.
	return "nm:" + strings.ToLower(strings.Join(strings.Fields(p.Name), " "))
}

// toEANPageSlug creates a URL-friendly slug from SCU and title.
func toEANPageSlug(scu, title string) string {
	// Prefer title if available, otherwise use SCU
	base := title
	if base == "" {
		base = scu
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
func mergeAttributes(a, b []model.KeyValue) []model.KeyValue {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		return cloneKeyValueSlice(b)
	}
	if len(b) == 0 {
		return cloneKeyValueSlice(a)
	}

	// Start with a copy of a
	result := cloneKeyValueSlice(a)
	// Add keys from b that don't exist in a
	for _, kv := range b {
		found := false
		for _, r := range result {
			if r.Key == kv.Key {
				found = true
				break
			}
		}
		if !found {
			result = append(result, kv)
		}
	}
	return result
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

// ComputeSeoURL builds seo_url for a SCU page: "/shop/{treePath}/{slug}"
// Uses treePathCache to avoid repeated DB calls.
func (r *EANPageRepo) ComputeSeoURL(slug string, categoryID int64, treePathCache map[int64][]string) string {
	if categoryID == 0 || r.CategoryRepo == nil {
		return "/shop/" + slug
	}

	treePath, cached := treePathCache[categoryID]
	if !cached {
		var err error
		treePath, err = r.CategoryRepo.GetTreePath(categoryID)
		if err != nil || len(treePath) == 0 {
			return "/shop/" + slug
		}
		treePathCache[categoryID] = treePath
	}

	return "/shop/" + strings.Join(treePath, "/") + "/" + slug
}

// BatchUpsertFromProducts creates or updates SCU pages from a batch of products.
// Returns a map of productID -> EANPageID for linking.
// This is much faster than calling UpsertFromProduct in a loop because:
// - Reads all existing SCU pages once (batch)
// - Writes all new/updated SCU pages in batch
// - Uses cached catalogizer tokens (call LoadCatalogizerCache() first)
// - Computes seo_url with cached category tree paths
func (r *EANPageRepo) BatchUpsertFromProducts(products []*model.Product) map[int64]int64 {
	if len(products) == 0 {
		return nil
	}

	// Collect all page keys and unique category IDs
	pageKeySet := make(map[string]struct{})
	catIDs := make(map[int64]struct{})
	for _, p := range products {
		if pk := eanPageKeyForProduct(p); pk != "" {
			pageKeySet[pk] = struct{}{}
		}
		if p.CategoryID != 0 {
			catIDs[p.CategoryID] = struct{}{}
		}
	}

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

	// Pre-compute tree paths for all category IDs (single pass)
	treePathCache := make(map[int64][]string)
	for catID := range catIDs {
		if r.CategoryRepo != nil {
			if tp, err := r.CategoryRepo.GetTreePath(catID); err == nil && len(tp) > 0 {
				treePathCache[catID] = tp
			}
		}
	}

	// Group products by SCU and merge into EANPage objects
	// New SCU pages
	newPages := make(map[string]*model.EANPage)
	// Existing SCU pages to update
	updatedPages := make(map[string]*model.EANPage)

	for _, p := range products {
		pk := eanPageKeyForProduct(p)
		if pk == "" {
			continue
		}

		if s, ok := existing[pk]; ok {
			// Update existing in memory
			updatedPages[pk] = s
		} else {
			// Create new with category from product
			if _, ok := newPages[pk]; !ok {
				slug := toEANPageSlug(pk, p.Name)
				newPages[pk] = &model.EANPage{
					EAN:          pk,
					Slug:         slug,
					Title:        parseTitleFromProductName(p.Name),
					Description:  p.Description,
					Content:      p.Description,
					Images:       limitStrings(deduplicateStrings(p.Images), maxEANPageImages),
					CategoryID:   p.CategoryID, // from product
					Brand:        p.Brand,
					BrandID:      p.BrandID,
					IsActive:     true,
					MinPrice:     p.Price,
					Currency:     p.Currency,
					Attributes:   mergeAttributes(nil, p.Attributes),
					ProductCount: 1,
					SeoURL:       r.ComputeSeoURL(slug, p.CategoryID, treePathCache),
					CreatedAt:    time.Now().Unix(),
					UpdatedAt:    time.Now().Unix(),
				}
			}
		}
	}

	// Merge updates for existing pages
	for _, p := range products {
		pk := eanPageKeyForProduct(p)
		if pk == "" {
			continue
		}
		if s, ok := updatedPages[pk]; ok {
			// Count products (no need to store IDs — link is in Product.EAN)
			s.ProductCount++

			// Update min_price
			if p.Price < s.MinPrice || s.MinPrice == 0 {
				s.MinPrice = p.Price
			}

			// Merge images with limit
			s.Images = limitStrings(mergeUniqueStrings(s.Images, p.Images), maxEANPageImages)

			// Merge attributes
			s.Attributes = mergeAttributes(s.Attributes, p.Attributes)

			// Update description if empty
			if s.Description == "" && p.Description != "" {
				s.Description = p.Description
				s.Content = p.Description
			}

			// Ensure SeoURL is set (compute if missing or category changed)
			if s.SeoURL == "" || s.CategoryID != p.CategoryID {
				s.CategoryID = p.CategoryID
				s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, treePathCache)
			}

			s.UpdatedAt = time.Now().Unix()
		}
	}

	// Create new pages
	created := make(map[string]int64)
	var newEANPageIDs []string
	for scu, s := range newPages {
		if err := r.CreateNoListIndex(s); err != nil {
			fmt.Printf("WARN: create eanpage for SCU %s: %v\n", scu, err)
			continue
		}
		created[scu] = s.ID
		newEANPageIDs = append(newEANPageIDs, KeyEANPage(s.ID))
	}

	// Batch add all new SCU pages to eanpage_list index (single write)
	if len(newEANPageIDs) > 0 {
		if _, err := r.Store.db.TurboPutBatchIndexString(TurboKeyEANPageList, newEANPageIDs); err != nil {
			fmt.Printf("WARN: batch add to eanpage_list: %v\n", err)
		}
	}

	// Update existing pages
	for scu, s := range updatedPages {
		data := MarshalEANPage(*s)
		if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
			fmt.Printf("WARN: update eanpage for SCU %s: %v\n", scu, err)
			continue
		}
		created[scu] = s.ID // reuse ID for mapping
	}

	fmt.Printf("[SCUPAGE]: start catologizator")
	// Catalogize new SCU pages using TurboTopNByIntersection
	if r.CatalogizeNew && r.Catalogizer != nil {
		// Get catalogizer via type assertion
		if catz, ok := r.Catalogizer.(interface {
			BuildEANTokens(scuPageID int64, name string) error
			CatalogizeEANPageByIntersection(scuPageID int64) (int64, error)
		}); ok {
			catalogized := 0
			for scu, s := range newPages {
				if s.CategoryID != 0 {
					continue
				}
				// Build tokens for this SCU page using all available text
				fullText := tokenizer.BuildEANTokensFullText(s.Title, s.Description, s.Content, s.Attributes)
				if err := catz.BuildEANTokens(s.ID, fullText); err != nil {
					fmt.Printf("WARN: build scu tokens for %s (id=%d): %v\n", scu, s.ID, err)
					continue
				}
				// Catalogize using TurboTopNByIntersection
				if catID, err := catz.CatalogizeEANPageByIntersection(s.ID); err == nil && catID > 0 {
					s.CategoryID = catID
					data := MarshalEANPage(*s)
					if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
						fmt.Printf("WARN: update eanpage category for %s (id=%d): %v\n", scu, s.ID, err)
					} else {
						catalogized++
					}
				}
			}
			if catalogized > 0 {
				fmt.Printf("[SCUPAGE] Catalogized %d new SCU pages via TurboTopNByIntersection\n", catalogized)
			}
		}
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

	return result
}

// RecalculateProductCounts recalculates ProductCount for all SCU pages
// based on actual products linked via SCU.
// This fixes inconsistencies after bulk imports.
func (r *EANPageRepo) RecalculateProductCounts() error {
	if r.Store == nil {
		return nil
	}

	all, err := r.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	updated := 0
	for i, sp := range all {
		if sp.EAN == "" {
			continue
		}

		// Count products with this SCU via turbo index
		key := "ean:" + sp.EAN
		tokens, err := r.Store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			// Index may not exist yet; skip
			continue
		}

		newCount := len(tokens)
		if newCount != sp.ProductCount {
			sp.ProductCount = newCount
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(sp)
			if err := r.Store.DocPut(KeyEANPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update product_count for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("[SCUPAGE] RecalculateProductCounts: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[SCUPAGE] RecalculateProductCounts: done. Updated %d SCU pages.\n", updated)
	return nil
}

// RecalculateMinPrices recalculates MinPrice for all SCU pages
// based on actual product prices linked via SCU.
// This fixes inconsistencies after bulk imports or price updates.
// productRepo is required to fetch product prices.
func (r *EANPageRepo) RecalculateMinPrices(productRepo *ProductRepo) error {
	if r.Store == nil || productRepo == nil {
		return nil
	}

	all, err := r.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// cacheSeo := map[int64][]string{}
	updated := 0
	for i := range all {
		sp := &all[i]
		if sp.EAN == "" {
			continue
		}

		// Get products with this SCU via turbo index
		key := "ean:" + sp.EAN
		tokens, err := r.Store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			// Index may not exist yet; skip
			continue
		}

		// Get all products at once
		docs, err := r.Store.db.MultiGetByDocIDs(tokens)
		if err != nil || len(docs) == 0 {
			continue
		}

		// Find minimum price among all products
		var minPrice float64
		found := false
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			p, err := UnmarshalProduct(doc)
			if err != nil {
				continue
			}
			if !found || p.Price < minPrice {
				minPrice = p.Price
				found = true
			}
		}

		//cacheSeo := map[int64][]string{}
		// sp.SeoURL = r.ComputeSeoURL(sp.Slug, sp.CategoryID, cacheSeo)
		// Update MinPrice if it changed
		if found && minPrice != sp.MinPrice {
			sp.MinPrice = minPrice
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(*sp)
			if err := r.Store.DocPut(KeyEANPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update min_price for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("[SCUPAGE] RecalculateMinPrices: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[SCUPAGE] RecalculateMinPrices: done. Updated %d SCU pages.\n", updated)
	return nil
}
