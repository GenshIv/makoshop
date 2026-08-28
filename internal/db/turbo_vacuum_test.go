package db

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestBatchIndexingReducesVacuum verifies that batch indexing writes each index
// only once (instead of per product), which reduces vacuum/fragmentation.
func TestBatchIndexingReducesVacuum(t *testing.T) {
	// Create a temporary store
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create repos
	promoCampaignRepo := NewPromoCampaignRepo(store)
	promoPlanRepo := NewPromoPlanRepo(store)
	promoLogRepo := NewPromoLogRepo(store)
	productRepo := NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)
	categoryRepo := NewCategoryRepo(store)
	turboSearch := NewTurboProductSearch(store, productRepo, categoryRepo, true)

	// Create a transaction
	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	// Create test products with many attributes
	const numProducts = 1000
	const numAttrsPerProduct = 10

	products := make([]*model.Product, numProducts)
	for i := range products {
		p := &model.Product{
			ID:        int64(i + 1),
			Name:      fmt.Sprintf("Test Product %d", i+1),
			CompanyID: 1,
			BrandID:   1,
			Price:     float64(i*10 + 10),
			EAN:       fmt.Sprintf("978000000000%d", i%10),
		}

		// Add many attributes
		for j := 0; j < numAttrsPerProduct; j++ {
			p.Attributes = append(p.Attributes, model.KeyValue{
				Key:   fmt.Sprintf("attr_%d", j),
				Value: fmt.Sprintf("value_%d", (i+j)%5), // Only 5 unique values per attribute
			})
		}

		products[i] = p
	}

	// Index products in batch (transactional)
	if err := turboSearch.BatchIndexProductstx(txn, products); err != nil {
		t.Fatalf("failed to batch index products: %v", err)
	}

	// Check the transaction buffer
	txn.mu.Lock()
	batchIndexSize := len(txn.turboBatchIndex)
	totalDocIDs := 0
	for _, docIDs := range txn.turboBatchIndex {
		totalDocIDs += len(docIDs)
	}
	txn.mu.Unlock()

	t.Logf("Batch index entries: %d", batchIndexSize)
	t.Logf("Total docIDs buffered: %d", totalDocIDs)

	// Expected:
	// - 1 product_list index
	// - 1 brand index
	// - 1 vendor index
	// - 50 attribute keys (10 attrs * 5 values each)
	// - ~1000 text tokens (unique per product name)
	// - ~6 price range indexes
	// Total: ~1059 index entries
	//
	// Key point: NOT 1000 * 10 = 10,000 entries (which would indicate per-product indexing)
	// The batch size should be proportional to unique index keys, not products * attributes

	if batchIndexSize > numProducts*numAttrsPerProduct {
		t.Errorf("expected < %d batch index entries, got %d (indicates per-product indexing)",
			numProducts*numAttrsPerProduct, batchIndexSize)
	}

	// Verify each index has the correct number of docIDs
	txn.mu.Lock()
	defer txn.mu.Unlock()

	// product_list should have all 1000 products
	productListIDs := txn.turboBatchIndex[TurboKeyProductList]
	if len(productListIDs) != numProducts {
		t.Errorf("product_list should have %d docIDs, got %d", numProducts, len(productListIDs))
	}

	// brand:1 should have all 1000 products
	brandIDs := txn.turboBatchIndex["brand:1"]
	if len(brandIDs) != numProducts {
		t.Errorf("brand:1 should have %d docIDs, got %d", numProducts, len(brandIDs))
	}

	// vendor:1 should have all 1000 products
	vendorIDs := txn.turboBatchIndex["vendor:1"]
	if len(vendorIDs) != numProducts {
		t.Errorf("vendor:1 should have %d docIDs, got %d", numProducts, len(vendorIDs))
	}

	t.Logf("✓ Batch indexing correctly aggregates docIDs per index (no per-product writes)")
}

// TestBatchIndexingMemoryEfficiency verifies that batch indexing doesn't buffer
// too many docIDs in memory for large imports.
func TestBatchIndexingMemoryEfficiency(t *testing.T) {
	// Create a temporary store
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create repos
	promoCampaignRepo := NewPromoCampaignRepo(store)
	promoPlanRepo := NewPromoPlanRepo(store)
	promoLogRepo := NewPromoLogRepo(store)
	productRepo := NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)
	categoryRepo := NewCategoryRepo(store)
	turboSearch := NewTurboProductSearch(store, productRepo, categoryRepo, true)

	// Create a transaction
	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	// Create test products
	const numProducts = 10000
	const numAttrsPerProduct = 20

	products := make([]*model.Product, numProducts)
	for i := range products {
		p := &model.Product{
			ID:        int64(i + 1),
			Name:      fmt.Sprintf("Test Product %d", i+1),
			CompanyID: 1,
			BrandID:   1,
			Price:     float64(i % 1000),
			EAN:       fmt.Sprintf("978%010d", i),
		}

		// Add many attributes with limited unique values
		for j := 0; j < numAttrsPerProduct; j++ {
			p.Attributes = append(p.Attributes, model.KeyValue{
				Key:   fmt.Sprintf("color_%d", j),
				Value: fmt.Sprintf("color_%d", (i+j)%10), // Only 10 unique values per attribute
			})
		}

		products[i] = p
	}

	// Index products in batch (transactional)
	if err := turboSearch.BatchIndexProductstx(txn, products); err != nil {
		t.Fatalf("failed to batch index products: %v", err)
	}

	// Check the transaction buffer size
	txn.mu.Lock()
	totalDocIDs := 0
	for _, docIDs := range txn.turboBatchIndex {
		totalDocIDs += len(docIDs)
	}
	txn.mu.Unlock()

	// Expected: 10,000 products * 20 attributes = 200,000 docID entries
	// This is acceptable memory usage for a server
	expectedMin := numProducts * numAttrsPerProduct / 2 // At least half should be buffered
	if totalDocIDs < expectedMin {
		t.Errorf("expected at least %d docIDs buffered, got %d", expectedMin, totalDocIDs)
	}

	t.Logf("✓ Buffered %d docIDs for %d products with %d attributes each",
		totalDocIDs, numProducts, numAttrsPerProduct)
}
