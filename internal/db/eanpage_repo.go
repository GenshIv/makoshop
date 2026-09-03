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
// Returns a space-separated string of keywords for catalogization.
func extractKeywordsFromProduct(p *model.Product) string {
	if p == nil {
		return ""
	}

	// Start with product name
	keywords := []string{p.Name}

	// Add shop category from Product.ShopCategory field if present
	if p.ShopCategory != "" {
		// Split by common delimiters and add each part
		parts := strings.Split(p.ShopCategory, ">")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				keywords = append(keywords, part)
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

// autoCatalogize determines the best category for a product based on anchor keywords.
// Uses pre-built token indexes from catalogizer (cat_tokens:{catID}).
// Returns category ID or 0 if no match found.
// Uses product name + shop_category (last element only) for matching.
func (r *EANPageRepo) autoCatalogize(p *model.Product) (int64, error) {
	if r.CategoryRepo == nil || r.Store == nil {
		return 0, nil
	}

	// Tokenize product name (same as catalogizer)
	productTokens := tokenizer.Tokenize(p.Name)

	// Also tokenize shop_category field if present (use only last element)
	if p.ShopCategory != "" {
		// Split by backslash or forward slash and take last element
		parts := strings.Split(p.ShopCategory, "\\")
		last := parts[len(parts)-1]
		last = strings.TrimSpace(last)
		if last != "" {
			catTokens := tokenizer.Tokenize(last)
			productTokens = append(productTokens, catTokens...)
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

// ProductEANIndexKey returns the key under which a product must be indexed in
// the "ean:" turbo index so GetProductsByEAN finds it on its EAN page.
// For products with an EAN that's the EAN; for name-based pages it's the same
// "nm:<name>" fallback as eanPageKeyForProduct (so pages and products always
// match). Returns "" when neither is available.
func ProductEANIndexKey(p *model.Product) string {
	if p.EAN != "" {
		return p.EAN
	}
	pk := eanPageKeyForProduct(p)
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

// BatchUpsertFromProducts creates or updates EAN pages from a batch of products.
// Returns a map of productID -> EANPageID for linking.
// This is much faster than calling UpsertFromProduct in a loop because:
// - Reads all existing EAN pages once (batch)
// - Writes all new/updated EAN pages in batch
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

	// Group products by EAN and merge into EANPage objects
	// New EAN pages
	newPages := make(map[string]*model.EANPage)
	// Existing EAN pages to update
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
			// Create new. Category resolution matches the single-path
			// UpsertFromProduct: prefer the product's category, and when the
			// product has none (0), fall back to the catalogizer so new pages
			// are not left uncategorized.
			if _, ok := newPages[pk]; !ok {
				slug := toEANPageSlug(pk, p.Name)
				categoryID := p.CategoryID
				if categoryID == 0 && r.CatalogizeNew && r.CategoryRepo != nil {
					if catID, err := r.autoCatalogize(p); err == nil && catID > 0 {
						categoryID = catID
					}
				}
				newPages[pk] = &model.EANPage{
					EAN:          pk,
					Slug:         slug,
					Title:        parseTitleFromProductName(p.Name),
					Description:  p.Description,
					Content:      p.Description,
					Images:       limitStrings(deduplicateStrings(p.Images), maxEANPageImages),
					CategoryID:   categoryID,
					Brand:        p.Brand,
					BrandID:      p.BrandID,
					IsActive:     true,
					MinPrice:     p.Price,
					Currency:     p.Currency,
					Attributes:   mergeAttributes(nil, p.Attributes),
					ProductCount: 1,
					SeoURL:       r.ComputeSeoURL(slug, categoryID, treePathCache),
					Keywords:     extractKeywordsFromProduct(p),
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

			// Update Keywords (always refresh with latest product info)
			s.Keywords = extractKeywordsFromProduct(p)

			// Category handling (matches the single-path
			// updateEANPageFromProduct "set if 0" semantics):
			//   - If the page has no category and the product does, adopt the
			//     product's category (this is how /admin/catalogize + rebuild
			//     restores pages that were reset to 0).
			//   - NEVER overwrite an existing (catalogizer-assigned) category
			//     with the product's: the product's CategoryID is usually 0 for
			//     price-file imports and would wipe the catalogizer's
			//     assignment on every rebuild/import.
			if s.CategoryID == 0 && p.CategoryID != 0 {
				s.CategoryID = p.CategoryID
				s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, treePathCache)
			} else if s.SeoURL == "" {
				s.SeoURL = r.ComputeSeoURL(s.Slug, s.CategoryID, treePathCache)
			}

			s.UpdatedAt = time.Now().Unix()
		}
	}

	// Create new pages
	created := make(map[string]int64)
	var newEANPageIDs []string
	for ean, s := range newPages {
		if err := r.CreateNoListIndex(s); err != nil {
			fmt.Printf("WARN: create eanpage for EAN %s: %v\n", ean, err)
			continue
		}
		created[ean] = s.ID
		newEANPageIDs = append(newEANPageIDs, KeyEANPage(s.ID))
	}

	// Batch add all new EAN pages to eanpage_list index (single write)
	if len(newEANPageIDs) > 0 {
		if _, err := r.Store.db.TurboPutBatchIndexString(TurboKeyEANPageList, newEANPageIDs); err != nil {
			fmt.Printf("WARN: batch add to eanpage_list: %v\n", err)
		}
	}

	// Update existing pages
	for ean, s := range updatedPages {
		data := MarshalEANPage(*s)
		if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
			fmt.Printf("WARN: update eanpage for EAN %s: %v\n", ean, err)
			continue
		}
		created[ean] = s.ID // reuse ID for mapping
	}

	fmt.Printf("[EANPAGE] CatalogizeNew=%v, Catalogizer=%v, newPages=%d\n", r.CatalogizeNew, r.Catalogizer != nil, len(newPages))
	// Catalogize new EAN pages using TurboTopNByIntersection
	if r.CatalogizeNew && r.Catalogizer != nil {
		// Get catalogizer via type assertion
		if catz, ok := r.Catalogizer.(interface {
			BuildEANTokens(scuPageID int64, name string) error
			CatalogizeEANPageByIntersection(scuPageID int64) (int64, error)
		}); ok {
			catalogized := 0
			for ean, s := range newPages {
				if s.CategoryID != 0 {
					continue
				}
				fmt.Printf("[EANPAGE] Attempting to catalogize %s (id=%d)\n", ean, s.ID)
				// Build tokens for this EAN page using Keywords field (product name + shop category)
				// Fall back to Title if Keywords is empty
				textForCatalogization := s.Keywords
				if textForCatalogization == "" {
					textForCatalogization = s.Title
				}
				if err := catz.BuildEANTokens(s.ID, textForCatalogization); err != nil {
					fmt.Printf("WARN: build ean tokens for %s (id=%d): %v\n", ean, s.ID, err)
					continue
				}
				// Catalogize using TurboTopNByIntersection

				matches := r.MatchProductToCategories(s.Keywords)
				fmt.Printf("[EANPAGE] Matched %d categories for %s (keywords=%s)\n", len(matches), ean, s.Keywords)
				newCatID := s.CategoryID
				if len(matches) > 0 {
					newCatID = matches[0].NewCategoryID
					fmt.Printf("[EANPAGE] Best match: cat %d (%s) with score %d\n", newCatID, matches[0].NewCategorySlug, matches[0].Score)
				}

				if newCatID != s.CategoryID {
					s.CategoryID = newCatID
					data := MarshalEANPage(*s)
					if err := r.Store.DocPut(KeyEANPage(s.ID), data); err != nil {
						fmt.Printf("WARN: update eanpage category for %s (id=%d): %v\n", ean, s.ID, err)
					} else {
						catalogized++
					}
				}
			}
			if catalogized > 0 {
				fmt.Printf("[EANPAGE] Catalogized %d new EAN pages via TurboTopNByIntersection\n", catalogized)
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

// CategoriesWithEANPages returns a set of category IDs that have at least one EAN page.
func (r *EANPageRepo) CategoriesWithEANPages() map[int64]struct{} {
	all, err := r.List()
	if err != nil {
		return nil
	}
	result := make(map[int64]struct{})
	for _, s := range all {
		if s.CategoryID != 0 {
			result[s.CategoryID] = struct{}{}
		}
	}
	return result
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

// RecalculateProductCounts recalculates ProductCount for all EAN pages
// based on actual products linked via EAN.
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

		// Count products with this EAN via turbo index
		key := "ean:" + sp.EAN
		tokens, err := r.Store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			// No products for this EAN — set count to 0
			if sp.ProductCount != 0 {
				sp.ProductCount = 0
				sp.UpdatedAt = time.Now().Unix()
				data := MarshalEANPage(sp)
				if err := r.Store.DocPut(KeyEANPage(sp.ID), data); err != nil {
					fmt.Printf("WARN: update product_count=0 for eanpage %d: %v\n", sp.ID, err)
					continue
				}
				updated++
			}
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
			fmt.Printf("[EANPAGE] RecalculateProductCounts: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[EANPAGE] RecalculateProductCounts: done. Updated %d EAN pages.\n", updated)
	return nil
}

// RecalculateMinPrices recalculates MinPrice for all EAN pages
// based on actual product prices linked via EAN.
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

		// Get products with this EAN via turbo index
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
			fmt.Printf("[EANPAGE] RecalculateMinPrices: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[EANPAGE] RecalculateMinPrices: done. Updated %d EAN pages.\n", updated)
	return nil
}

// BatchUpsertFromProductsTx is the transactional version of BatchUpsertFromProducts.
// It buffers all writes in the transaction instead of writing directly to the DB.
func (r *EANPageRepo) BatchUpsertFromProductsTx(txn *Transaction, products []*model.Product) map[int64]int64 {
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

	// Group products by EAN and merge into EANPage objects
	newPages := make(map[string]*model.EANPage)
	updatedPages := make(map[string]*model.EANPage)

	for _, p := range products {
		pk := eanPageKeyForProduct(p)
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
		pk := eanPageKeyForProduct(p)
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

	return result
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

	return nil
}

// RecalculateProductCountsTx is the transactional version of RecalculateProductCounts.
func (r *EANPageRepo) RecalculateProductCountsTx(txn *Transaction) error {
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

		key := "ean:" + sp.EAN
		tokens, err := r.Store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			continue
		}

		newCount := len(tokens)
		if newCount != sp.ProductCount {
			sp.ProductCount = newCount
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(sp)
			if err := txn.DocPut(KeyEANPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update product_count for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("[EANPAGE] RecalculateProductCountsTx: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[EANPAGE] RecalculateProductCountsTx: done. Updated %d EAN pages.\n", updated)
	return nil
}

// RecalculateMinPricesTx is the transactional version of RecalculateMinPrices.
func (r *EANPageRepo) RecalculateMinPricesTx(txn *Transaction, productRepo *ProductRepo) error {
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

		key := "ean:" + sp.EAN
		tokens, err := r.Store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			continue
		}

		docs, err := r.Store.db.MultiGetByDocIDs(tokens)
		if err != nil || len(docs) == 0 {
			continue
		}

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

		if found && minPrice != sp.MinPrice {
			sp.MinPrice = minPrice
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(sp)
			if err := txn.DocPut(KeyEANPage(sp.ID), data); err != nil {
				fmt.Printf("WARN: update min_price for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("[EANPAGE] RecalculateMinPricesTx: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	fmt.Printf("[EANPAGE] RecalculateMinPricesTx: done. Updated %d EAN pages.\n", updated)
	return nil
}
