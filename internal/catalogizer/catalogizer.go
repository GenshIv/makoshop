package catalogizer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GenshIv/intHache"
	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/tokenizer"
)

const (
	turboKeyCatTokens   = "cat_tokens:"      // cat_tokens:{catID} -> [hashes] (key-value)
	turboKeySCUTokens   = "scu_tokens:"      // scu_tokens:{scuPageID} -> [hashes] (turbo index)
	turboKeyQueryTokens = "tmp_query_tokens" // temporary index for catalogization queries
)

// Catalogizer provides automatic category assignment for products based on
// token-based matching (word hashes from anchor_keywords in each category).
type Catalogizer struct {
	store        *db.Store
	categoryRepo *db.CategoryRepo
	productRepo  *db.ProductRepo
}

func New(store *db.Store, categoryRepo *db.CategoryRepo, productRepo *db.ProductRepo) *Catalogizer {
	return &Catalogizer{
		store:        store,
		categoryRepo: categoryRepo,
		productRepo:  productRepo,
	}
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

// BuildTokensForCategory builds token index for a single category from its anchor_keywords.
// Anchor keywords are already stems (root words). Hash each keyword with the same tokenizer.
func (c *Catalogizer) BuildTokensForCategory(cat *model.Category) error {
	if cat == nil || !cat.IsActive || len(cat.AnchorKeywords) == 0 {
		if err := c.store.TurboWrite(turboKeyCatTokens+fmt.Sprintf("%d", cat.ID), []byte{}); err != nil {
			return fmt.Errorf("clear tokens for cat %d: %w", cat.ID, err)
		}
		return nil
	}

	// Hash each keyword directly (no double tokenization)
	hashSet := make(map[uint64]bool)
	for _, kw := range cat.AnchorKeywords {
		if kw == "" {
			continue
		}
		// Use same tokenization as for product names
		tokens := tokenizer.Tokenize(kw)
		for _, t := range tokens {
			hashSet[t.Hash] = true
		}
	}

	var hashes []uint64
	for h := range hashSet {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })

	if len(hashes) == 0 {
		return c.store.TurboWrite(turboKeyCatTokens+fmt.Sprintf("%d", cat.ID), []byte{})
	}

	// Convert uint64 to key128 for makodb
	key128s := make([]intHache.Key128, len(hashes))
	for i, h := range hashes {
		key128s[i] = intHache.Key128{h, 0}
	}

	buf := makodb.TurboBinaryNew(key128s)
	return c.store.TurboWrite(turboKeyCatTokens+fmt.Sprintf("%d", cat.ID), buf)
}

// RebuildAllCategoryTokens rebuilds token indexes for all active categories.
func (c *Catalogizer) RebuildAllCategoryTokens() error {
	categories, err := c.categoryRepo.ListAll()
	if err != nil {
		return fmt.Errorf("list categories: %w", err)
	}

	count := 0
	for _, cat := range categories {
		if cat.IsActive && len(cat.AnchorKeywords) > 0 {
			if err := c.BuildTokensForCategory(&cat); err != nil {
				fmt.Printf("[CATALOGIZER] Error building tokens for cat %d: %v\n", cat.ID, err)
			} else {
				count++
			}
		}
	}
	fmt.Printf("[CATALOGIZER] Rebuilt tokens for %d categories\n", count)
	return nil
}

// CatalogizeProduct determines the best category for a product based on token matching.
func (c *Catalogizer) CatalogizeProduct(p *model.Product) (*CatalogizeResult, error) {
	productTokens := tokenizer.Tokenize(p.Name)
	if len(productTokens) == 0 {
		return nil, nil
	}

	productHashes := make([]uint64, len(productTokens))
	productWords := make(map[uint64]string)
	for i, t := range productTokens {
		productHashes[i] = t.Hash
		productWords[t.Hash] = t.Word
	}

	categories, err := c.categoryRepo.ListAll()
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}

	var best *CatalogizeResult

	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		data, err := c.store.DB().TurboRawRead(turboKeyCatTokens + fmt.Sprintf("%d", cat.ID))
		if err != nil || len(data) == 0 {
			continue
		}

		catTokensKey128 := makodb.TurboUnsafeReadTokens(data)
		// Convert key128 to uint64
		catTokens := make([]uint64, len(catTokensKey128))
		for i, t := range catTokensKey128 {
			catTokens[i] = t[0]
		}

		overlap := tokenizer.CountTokenOverlap(productHashes, catTokens)

		if overlap <= 0 {
			continue
		}

		var matched []string
		catSet := make(map[uint64]bool)
		for _, h := range catTokens {
			catSet[h] = true
		}
		for _, h := range productHashes {
			if catSet[h] && productWords[h] != "" {
				matched = append(matched, productWords[h])
			}
		}

		result := &CatalogizeResult{
			ProductID:       p.ID,
			OldCategoryID:   p.CategoryID,
			NewCategoryID:   cat.ID,
			NewCategorySlug: cat.Slug,
			Score:           overlap,
			MatchedTokens:   matched,
		}

		if best == nil || overlap > best.Score {
			best = result
		}
	}

	return best, nil
}

// MatchProductToCategories returns ALL matching categories sorted by score (descending).
// Used for the test tool in admin panel.
func (c *Catalogizer) MatchProductToCategories(name string) []CatalogizeResult {
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

	categories, err := c.categoryRepo.ListAll()
	if err != nil {
		return nil
	}

	var results []CatalogizeResult

	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		data, err := c.store.DB().TurboRawRead(turboKeyCatTokens + fmt.Sprintf("%d", cat.ID))
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

// ApplyCategory updates the product's category.
func (c *Catalogizer) ApplyCategory(p *model.Product, result *CatalogizeResult) error {
	if result == nil || result.NewCategoryID == p.CategoryID {
		return nil
	}
	return c.productRepo.Update(p.ID, func(prod *model.Product) {
		prod.CategoryID = result.NewCategoryID
	})
}

// BatchCatalogize processes multiple products.
func (c *Catalogizer) BatchCatalogize(productIDs []int64, apply bool) ([]CatalogizeResult, error) {
	var results []CatalogizeResult
	for _, id := range productIDs {
		p, err := c.productRepo.Get(id)
		if err != nil {
			continue
		}
		result, err := c.CatalogizeProduct(p)
		if err != nil {
			fmt.Printf("WARN: catalogize product %d: %v\n", id, err)
			continue
		}
		if result != nil {
			if apply {
				if err := c.ApplyCategory(p, result); err != nil {
					fmt.Printf("WARN: apply category for product %d: %v\n", id, err)
				}
			}
			results = append(results, *result)
		}
	}
	return results, nil
}

// TokenizeName returns tokens for a given name (for debugging).
func TokenizeName(name string) []tokenizer.TokenInfo {
	return tokenizer.Tokenize(name)
}

// StemWord returns the stem of a word (for debugging).
func StemWord(word string) string {
	// Use tokenizer internal function via public wrapper
	tokens := tokenizer.Tokenize(word)
	if len(tokens) > 0 {
		return tokens[0].Word
	}
	return word
}

// BuildEANTokens builds a Turbo index of tokens for an SCU page.
// This is used with TurboTopNByIntersection for fast catalogization.
// Uses Title + Description + Content + Attributes for better token coverage.
func (c *Catalogizer) BuildEANTokens(scuPageID int64, name string) error {
	tokens := tokenizer.Tokenize(name)
	if len(tokens) == 0 {
		return nil
	}

	hashes := make([]uint64, len(tokens))
	for i, t := range tokens {
		hashes[i] = t.Hash
	}

	key := turboKeySCUTokens + fmt.Sprintf("%d", scuPageID)
	_ = c.store.DB().TurboClearIndex(key)
	// Convert uint64 hashes to strings for TurboPutBatchIndexString
	strHashes := make([]string, len(hashes))
	for i, h := range hashes {
		strHashes[i] = fmt.Sprintf("%d", h)
	}
	if _, err := c.store.DB().TurboPutBatchIndexString(key, strHashes); err != nil {
		return fmt.Errorf("turbo index scu_tokens for scu %d: %w", scuPageID, err)
	}
	return nil
}

// CatalogizeEANPageByIntersection uses TurboTopNByIntersection to find the best category
// for an SCU page based on token overlap with category anchor_keywords.
// Returns the best category ID or 0 if no match. Returns no error for missing keys.
func (c *Catalogizer) CatalogizeEANPageByIntersection(scuPageID int64) (int64, error) {
	eanKey := turboKeySCUTokens + fmt.Sprintf("%d", scuPageID)

	// Collect all active category token keys
	categories, err := c.categoryRepo.ListAll()
	if err != nil {
		return 0, fmt.Errorf("list categories: %w", err)
	}

	var candidateKeys []string
	catIDByKey := make(map[string]int64)
	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}
		key := turboKeyCatTokens + fmt.Sprintf("%d", cat.ID)
		candidateKeys = append(candidateKeys, key)
		catIDByKey[key] = cat.ID
	}

	if len(candidateKeys) == 0 {
		return 0, nil
	}

	// Use TurboTopNByIntersection to find categories with most overlapping tokens
	results, err := c.store.DB().TurboTopNByIntersection(eanKey, candidateKeys, 10)
	if err != nil {
		// Missing key (no tokens for this SCU page) is not an error
		if strings.Contains(err.Error(), "key not found") {
			return 0, nil
		}
		return 0, fmt.Errorf("turbo topN intersection: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	// Return the category with the highest count (first result)
	// Convert results[0].Key to string for map lookup
	//if catID, ok := catIDByKey[results[0].Key]; ok {
	//	return catID, nil
	//}
	return 0, nil
}
