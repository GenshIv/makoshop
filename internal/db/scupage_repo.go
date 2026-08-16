package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/tokenizer"
)

const (
	TurboKeySCUPageList = "scupage_list"
	turboKeySCUPageSCU  = "scupage_scu:"  // prefix for SCU lookup
	turboKeySCUPageSlug = "scupage_slug:" // prefix for slug lookup

	// Limits to prevent SCU pages from growing too large in DB
	maxSCUPageProductIDs = 500 // max product IDs to store in SCUPage
	maxSCUPageImages     = 50  // max images to store in SCUPage
)

type SCUPageRepo struct {
	Store            *Store
	CategoryRepo     *CategoryRepo
	CatalogizeNew    bool                             // if true, auto-catalogize new SCU pages
	Catalogizer      interface{}                      // *catalogizer.Catalogizer (interface to avoid import cycle)
	catalogizerCache []tokenizer.CachedCategoryTokens // pre-loaded for batch operations
}

func NewSCUPageRepo(store *Store) *SCUPageRepo {
	return &SCUPageRepo{Store: store}
}

// SetCategoryRepo attaches a CategoryRepo for auto-catalogization.
func (r *SCUPageRepo) SetCategoryRepo(cr *CategoryRepo) {
	r.CategoryRepo = cr
}

// EnableCatalogizeNew enables auto-catalogization for new SCU pages.
func (r *SCUPageRepo) EnableCatalogizeNew(enabled bool) {
	r.CatalogizeNew = enabled
}

// LoadCatalogizerCache loads category token indexes for fast batch auto-catalogization.
// Call this before BatchUpsertFromProducts to avoid repeated TurboRawRead calls.
func (r *SCUPageRepo) LoadCatalogizerCache() error {
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

		tokens := makodb.TurboUnsafeReadTokens(data)
		if len(tokens) == 0 {
			continue
		}

		cached = append(cached, tokenizer.CachedCategoryTokens{
			ID:       cat.ID,
			IsActive: true,
			Tokens:   tokens,
		})
	}

	r.catalogizerCache = cached
	fmt.Printf("[SCUPAGE] Loaded catalogizer cache: %d categories with tokens\n", len(cached))
	return nil
}

// CreateNoListIndex creates a new SCU page WITHOUT adding to scupage_list index.
// Used in batch operations where list index is updated once after all creations.
func (r *SCUPageRepo) CreateNoListIndex(s *model.SCUPage) error {
	if s.SCU == "" {
		return fmt.Errorf("scu is required")
	}

	id, err := r.Store.NextID("scupage")
	if err != nil {
		return fmt.Errorf("next_id scupage: %w", err)
	}
	s.ID = id
	s.CreatedAt = time.Now().Unix()
	s.UpdatedAt = time.Now().Unix()
	if s.Slug == "" {
		s.Slug = toSCUPageSlug(s.SCU, s.Title)
	}

	data := MarshalSCUPage(*s)
	if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
		return fmt.Errorf("save scupage: %w", err)
	}

	// Turbo index: scupage_scu:<scu>
	scuKey := turboKeySCUPageSCU + s.SCU
	if err := r.Store.TurboWrite(scuKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_ = r.Store.DocDelete(KeySCUPage(s.ID))
		return fmt.Errorf("turbo index scupage_scu: %w", err)
	}

	// Turbo index: scupage_slug:<slug>
	slugKey := turboKeySCUPageSlug + s.Slug
	if err := r.Store.TurboWrite(slugKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_ = r.Store.TurboWrite(scuKey, []byte{})
		_ = r.Store.DocDelete(KeySCUPage(s.ID))
		return fmt.Errorf("turbo index scupage_slug: %w", err)
	}

	return nil
}

// Create creates a new SCU page.
func (r *SCUPageRepo) Create(s *model.SCUPage) error {
	if s.SCU == "" {
		return fmt.Errorf("scu is required")
	}

	id, err := r.Store.NextID("scupage")
	if err != nil {
		return fmt.Errorf("next_id scupage: %w", err)
	}
	s.ID = id
	s.CreatedAt = time.Now().Unix()
	s.UpdatedAt = time.Now().Unix()
	if s.Slug == "" {
		s.Slug = toSCUPageSlug(s.SCU, s.Title)
	}

	data := MarshalSCUPage(*s)
	if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
		return fmt.Errorf("save scupage: %w", err)
	}

	// Turbo index: scupage_list
	if _, err := r.Store.db.TurboPutIndex(TurboKeySCUPageList, uint64(id)); err != nil {
		_ = r.Store.DocDelete(KeySCUPage(s.ID))
		return fmt.Errorf("turbo index scupage_list: %w", err)
	}

	// Turbo index: scupage_scu:<scu>
	scuKey := turboKeySCUPageSCU + s.SCU
	if err := r.Store.TurboWrite(scuKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndex(TurboKeySCUPageList, uint64(id))
		_ = r.Store.DocDelete(KeySCUPage(s.ID))
		return fmt.Errorf("turbo index scupage_scu: %w", err)
	}

	// Turbo index: scupage_slug:<slug>
	slugKey := turboKeySCUPageSlug + s.Slug
	if err := r.Store.TurboWrite(slugKey, []byte(strconv.FormatInt(id, 10))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndex(TurboKeySCUPageList, uint64(id))
		_ = r.Store.TurboWrite(scuKey, []byte{})
		_ = r.Store.DocDelete(KeySCUPage(s.ID))
		return fmt.Errorf("turbo index scupage_slug: %w", err)
	}

	return nil
}

// Get returns a SCU page by ID.
func (r *SCUPageRepo) Get(id int64) (*model.SCUPage, error) {
	data, err := r.Store.DocGet(KeySCUPage(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("scu page %d not found", id)
		}
		return nil, fmt.Errorf("get scupage %d: %w", id, err)
	}
	return UnmarshalSCUPage(data)
}

// GetBySCU returns a SCU page by SCU.
func (r *SCUPageRepo) GetBySCU(scu string) (*model.SCUPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is empty")
	}
	scuKey := turboKeySCUPageSCU + scu
	data, err := r.Store.db.TurboRawRead(scuKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("scu page with scu %q not found", scu)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetBySlug returns a SCU page by slug.
func (r *SCUPageRepo) GetBySlug(slug string) (*model.SCUPage, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is empty")
	}
	slugKey := turboKeySCUPageSlug + slug
	data, err := r.Store.db.TurboRawRead(slugKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("scu page with slug %q not found", slug)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// Update updates a SCU page.
func (r *SCUPageRepo) Update(id int64, updater func(*model.SCUPage)) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSCU := s.SCU
	oldSlug := s.Slug
	updater(s)
	s.UpdatedAt = time.Now().Unix()

	data := MarshalSCUPage(*s)
	if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
		return fmt.Errorf("update scupage: %w", err)
	}

	// Update SCU index if changed
	if oldSCU != s.SCU {
		_ = r.Store.TurboWrite(turboKeySCUPageSCU+oldSCU, []byte{})
		if s.SCU != "" {
			if err := r.Store.TurboWrite(turboKeySCUPageSCU+s.SCU, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update scupage_scu index: %w", err)
			}
		}
	}

	// Update slug index if changed
	if oldSlug != s.Slug {
		_ = r.Store.TurboWrite(turboKeySCUPageSlug+oldSlug, []byte{})
		if s.Slug != "" {
			if err := r.Store.TurboWrite(turboKeySCUPageSlug+s.Slug, []byte(strconv.FormatInt(id, 10))); err != nil {
				return fmt.Errorf("update scupage_slug index: %w", err)
			}
		}
	}

	return nil
}

// List returns all SCU pages via turbo index.
func (r *SCUPageRepo) List() ([]model.SCUPage, error) {
	data, err := r.Store.db.TurboRawRead(TurboKeySCUPageList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.SCUPage
	for _, id := range ids {
		s, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *s)
	}
	return result, nil
}

// ListAll returns all SCU pages (alias for List).
func (r *SCUPageRepo) ListAll() ([]model.SCUPage, error) {
	return r.List()
}

// Delete removes a SCU page.
func (r *SCUPageRepo) Delete(id int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove turbo indexes
	_, _ = r.Store.db.TurboDeleteIndex(TurboKeySCUPageList, uint64(id))
	if s.SCU != "" {
		_ = r.Store.TurboWrite(turboKeySCUPageSCU+s.SCU, []byte{})
	}
	if s.Slug != "" {
		_ = r.Store.TurboWrite(turboKeySCUPageSlug+s.Slug, []byte{})
	}

	if err := r.Store.DocDelete(KeySCUPage(id)); err != nil {
		return fmt.Errorf("delete scupage: %w", err)
	}
	return nil
}

// AddProduct increments product count for this SCU page.
// NOTE: Product→SCU link is stored in Product.SCU field.
// SCU→Products query via turbo index "scu:{scu}" in TurboProductSearch.
func (r *SCUPageRepo) AddProduct(id int64, productID int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}
	s.ProductCount++
	s.UpdatedAt = time.Now().Unix()
	data := MarshalSCUPage(*s)
	return r.Store.DocPut(KeySCUPage(s.ID), data)
}

// RemoveProduct decrements product count for this SCU page.
// NOTE: Product→SCU link is stored in Product.SCU field.
// SCU→Products query via turbo index "scu:{scu}" in TurboProductSearch.
func (r *SCUPageRepo) RemoveProduct(id int64, productID int64) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}
	if s.ProductCount > 0 {
		s.ProductCount--
	}
	s.UpdatedAt = time.Now().Unix()
	data := MarshalSCUPage(*s)
	return r.Store.DocPut(KeySCUPage(s.ID), data)
}

// UpsertBySCU creates or updates a SCU page by SCU.
// If exists: updates fields if new values are provided.
// If not: creates new.
func (r *SCUPageRepo) UpsertBySCU(scu string, updater func(*model.SCUPage)) (*model.SCUPage, error) {
	if scu == "" {
		return nil, fmt.Errorf("scu is required")
	}

	// Try to get existing
	s, err := r.GetBySCU(scu)
	if err == nil {
		// Update existing
		updater(s)
		s.UpdatedAt = time.Now().Unix()
		data := MarshalSCUPage(*s)
		if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
			return nil, fmt.Errorf("update scupage: %w", err)
		}
		return s, nil
	}

	// Create new
	s = &model.SCUPage{
		SCU:      scu,
		IsActive: true,
	}
	updater(s)
	if s.Slug == "" {
		s.Slug = toSCUPageSlug(s.SCU, s.Title)
	}
	if err := r.Create(s); err != nil {
		return nil, err
	}
	return s, nil
}

// UpsertFromProduct creates or updates a SCU page from a product.
// This is the main entry point for import-time indexing.
// Merges attributes, images, and updates min_price.
func (r *SCUPageRepo) UpsertFromProduct(product *model.Product) error {
	if product.SCU == "" {
		return nil
	}

	// Try to get existing SCU page
	s, err := r.GetBySCU(product.SCU)
	if err == nil {
		// Update existing: merge data
		return r.updateSCUPageFromProduct(s, product)
	}

	// Create new SCU page from product
	// Category is determined ONLY by catalogizer (anchor keywords), ignoring product.CategoryID.
	categoryID := int64(0)
	if r.CatalogizeNew && r.CategoryRepo != nil {
		if catID, err := r.autoCatalogize(product); err == nil && catID > 0 {
			categoryID = catID
		}
	}

	s = &model.SCUPage{
		SCU:          product.SCU,
		Slug:         toSCUPageSlug(product.SCU, product.Name),
		Title:        parseTitleFromProductName(product.Name),
		Description:  product.Description,
		Content:      product.Description,
		Images:       limitStrings(deduplicateStrings(product.Images), maxSCUPageImages),
		CategoryID:   categoryID,
		Brand:        product.Brand,
		BrandID:      product.BrandID,
		IsActive:     true,
		MinPrice:     product.Price,
		Currency:     product.Currency,
		Attributes:   mergeAttributes(nil, product.Attributes),
		ProductCount: 1,
		CreatedAt:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}

	return r.Create(s)
}

// updateSCUPageFromProduct merges product data into an existing SCU page.
func (r *SCUPageRepo) updateSCUPageFromProduct(s *model.SCUPage, product *model.Product) error {
	// Increment product count (link is in Product.SCU, not stored here)
	s.ProductCount++

	// Update min_price
	if product.Price < s.MinPrice || s.MinPrice == 0 {
		s.MinPrice = product.Price
	}

	// Merge images (unique, limited)
	s.Images = limitStrings(mergeUniqueStrings(s.Images, product.Images), maxSCUPageImages)

	// Merge attributes (no duplicates)
	s.Attributes = mergeAttributes(s.Attributes, product.Attributes)

	// Update description if empty
	if s.Description == "" && product.Description != "" {
		s.Description = product.Description
		s.Content = product.Description
	}

	s.UpdatedAt = time.Now().Unix()

	// Save
	data := MarshalSCUPage(*s)
	return r.Store.DocPut(KeySCUPage(s.ID), data)
}

// LinkProductBySCU finds or creates a SCU page for the given SCU and links the product.
// This is the main entry point for import-time linking.
func (r *SCUPageRepo) LinkProductBySCU(scu string, product *model.Product) error {
	if scu == "" {
		return nil
	}

	// Try to find existing SCU page
	s, err := r.GetBySCU(scu)
	if err == nil {
		// Exists — just link product
		return r.AddProduct(s.ID, product.ID)
	}

	// Create new SCU page from product data
	s, err = r.UpsertBySCU(scu, func(s *model.SCUPage) {
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
func (r *SCUPageRepo) autoCatalogize(p *model.Product) (int64, error) {
	if r.CategoryRepo == nil || r.Store == nil {
		return 0, nil
	}

	// Tokenize product name (same as catalogizer)
	productTokens := tokenizer.Tokenize(p.Name)
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
		data, err := r.Store.DB().TurboRawRead("cat_tokens:" + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(data) == 0 {
			continue
		}

		catTokens := makodb.TurboUnsafeReadTokens(data)
		if len(catTokens) == 0 {
			continue
		}

		// Count overlap using set
		catSet := make(map[uint64]bool, len(catTokens))
		for _, h := range catTokens {
			catSet[h] = true
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

// toSCUPageSlug creates a URL-friendly slug from SCU and title.
func toSCUPageSlug(scu, title string) string {
	// Prefer title if available, otherwise use SCU
	base := title
	if base == "" {
		base = scu
	}
	return toSlug(base)
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

// BatchUpsertFromProducts creates or updates SCU pages from a batch of products.
// Returns a map of productID -> SCUPageID for linking.
// This is much faster than calling UpsertFromProduct in a loop because:
// - Reads all existing SCU pages once (batch)
// - Writes all new/updated SCU pages in batch
// - Uses cached catalogizer tokens (call LoadCatalogizerCache() first)
func (r *SCUPageRepo) BatchUpsertFromProducts(products []*model.Product) map[int64]int64 {
	if len(products) == 0 {
		return nil
	}

	// Collect all SCUs
	scuSet := make(map[string]struct{})
	for _, p := range products {
		if p.SCU != "" {
			scuSet[p.SCU] = struct{}{}
		}
	}

	// Batch load existing SCU pages
	existing := make(map[string]*model.SCUPage)
	for scu := range scuSet {
		if s, err := r.GetBySCU(scu); err == nil {
			existing[scu] = s
		}
	}

	// Group products by SCU and merge into SCUPage objects
	// New SCU pages
	newPages := make(map[string]*model.SCUPage)
	// Existing SCU pages to update
	updatedPages := make(map[string]*model.SCUPage)

	for _, p := range products {
		if p.SCU == "" {
			continue
		}

		if s, ok := existing[p.SCU]; ok {
			// Update existing in memory
			updatedPages[p.SCU] = s
		} else {
			// Create new with category from product
			if _, ok := newPages[p.SCU]; !ok {
				newPages[p.SCU] = &model.SCUPage{
					SCU:          p.SCU,
					Slug:         toSCUPageSlug(p.SCU, p.Name),
					Title:        parseTitleFromProductName(p.Name),
					Description:  p.Description,
					Content:      p.Description,
					Images:       limitStrings(deduplicateStrings(p.Images), maxSCUPageImages),
					CategoryID:   p.CategoryID, // from product
					Brand:        p.Brand,
					BrandID:      p.BrandID,
					IsActive:     true,
					MinPrice:     p.Price,
					Currency:     p.Currency,
					Attributes:   mergeAttributes(nil, p.Attributes),
					ProductCount: 1,
					CreatedAt:    time.Now().Unix(),
					UpdatedAt:    time.Now().Unix(),
				}
			}
		}
	}

	// Merge updates for existing pages
	for _, p := range products {
		if p.SCU == "" {
			continue
		}
		if s, ok := updatedPages[p.SCU]; ok {
			// Count products (no need to store IDs — link is in Product.SCU)
			s.ProductCount++

			// Update min_price
			if p.Price < s.MinPrice || s.MinPrice == 0 {
				s.MinPrice = p.Price
			}

			// Merge images with limit
			s.Images = limitStrings(mergeUniqueStrings(s.Images, p.Images), maxSCUPageImages)

			// Merge attributes
			s.Attributes = mergeAttributes(s.Attributes, p.Attributes)

			// Update description if empty
			if s.Description == "" && p.Description != "" {
				s.Description = p.Description
				s.Content = p.Description
			}

			s.UpdatedAt = time.Now().Unix()
		}
	}

	// Create new pages
	created := make(map[string]int64)
	var newSCUPageIDs []uint64
	for scu, s := range newPages {
		if err := r.CreateNoListIndex(s); err != nil {
			fmt.Printf("WARN: create scupage for SCU %s: %v\n", scu, err)
			continue
		}
		created[scu] = s.ID
		newSCUPageIDs = append(newSCUPageIDs, uint64(s.ID))
	}

	// Batch add all new SCU pages to scupage_list index (single write)
	if len(newSCUPageIDs) > 0 {
		if _, err := r.Store.db.TurboPutBatchIndex(TurboKeySCUPageList, newSCUPageIDs); err != nil {
			fmt.Printf("WARN: batch add to scupage_list: %v\n", err)
		}
	}

	// Update existing pages
	for scu, s := range updatedPages {
		data := MarshalSCUPage(*s)
		if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
			fmt.Printf("WARN: update scupage for SCU %s: %v\n", scu, err)
			continue
		}
		created[scu] = s.ID // reuse ID for mapping
	}

	fmt.Printf("[SCUPAGE]: start catologizator")
	// Catalogize new SCU pages using TurboTopNByIntersection
	if r.CatalogizeNew && r.Catalogizer != nil {
		// Get catalogizer via type assertion
		if catz, ok := r.Catalogizer.(interface {
			BuildSCUTokens(scuPageID int64, name string) error
			CatalogizeSCUPageByIntersection(scuPageID int64) (int64, error)
		}); ok {
			catalogized := 0
			for scu, s := range newPages {
				if s.CategoryID != 0 {
					continue
				}
				// Build tokens for this SCU page using all available text
				fullText := tokenizer.BuildSCUTokensFullText(s.Title, s.Description, s.Content, s.Attributes)
				if err := catz.BuildSCUTokens(s.ID, fullText); err != nil {
					fmt.Printf("WARN: build scu tokens for %s (id=%d): %v\n", scu, s.ID, err)
					continue
				}
				// Catalogize using TurboTopNByIntersection
				if catID, err := catz.CatalogizeSCUPageByIntersection(s.ID); err == nil && catID > 0 {
					s.CategoryID = catID
					data := MarshalSCUPage(*s)
					if err := r.Store.DocPut(KeySCUPage(s.ID), data); err != nil {
						fmt.Printf("WARN: update scupage category for %s (id=%d): %v\n", scu, s.ID, err)
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

	// Build productID -> SCUPageID map
	result := make(map[int64]int64)
	for _, p := range products {
		if p.SCU == "" {
			continue
		}
		if id, ok := created[p.SCU]; ok {
			result[p.ID] = id
		}
	}

	return result
}

// RecalculateProductCounts recalculates ProductCount for all SCU pages
// based on actual products linked via SCU.
// This fixes inconsistencies after bulk imports.
func (r *SCUPageRepo) RecalculateProductCounts() error {
	if r.Store == nil {
		return nil
	}

	all, err := r.List()
	if err != nil {
		return fmt.Errorf("list scupages: %w", err)
	}

	updated := 0
	for i, sp := range all {
		if sp.SCU == "" {
			continue
		}

		// Count products with this SCU via turbo index
		key := "scu:" + sp.SCU
		data, err := r.Store.db.TurboRawRead(key)
		if err != nil || len(data) == 0 {
			// Index may not exist yet; skip
			continue
		}

		products := makodb.TurboUnsafeReadTokens(data)
		newCount := len(products)
		if newCount != sp.ProductCount {
			sp.ProductCount = newCount
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalSCUPage(sp)
			if err := r.Store.DocPut(KeySCUPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update product_count for scupage %d: %v\n", sp.ID, err)
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
