package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestCompanyStatusFiltering verifies that products from inactive companies
// are excluded from catalog grouping and cleaned up.
func TestCompanyStatusFiltering(t *testing.T) {
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

	companyRepo := NewCompanyRepo(store)
	productRepo := NewProductRepo(store, nil, nil, nil)

	// Create companies with different statuses
	activeCompany := &model.Company{Name: "Active Corp", Status: model.CompanyStatusVerified}
	if err := companyRepo.Create(activeCompany); err != nil {
		t.Fatalf("create active company: %v", err)
	}

	blockedCompany := &model.Company{Name: "Blocked Corp", Status: model.CompanyStatusBlocked}
	if err := companyRepo.Create(blockedCompany); err != nil {
		t.Fatalf("create blocked company: %v", err)
	}

	pendingCompany := &model.Company{Name: "Pending Corp", Status: model.CompanyStatusPending}
	if err := companyRepo.Create(pendingCompany); err != nil {
		t.Fatalf("create pending company: %v", err)
	}

	// Verify IsCompanyActive
	if !companyRepo.IsCompanyActive(activeCompany.ID) {
		t.Error("active company should be considered active")
	}
	if companyRepo.IsCompanyActive(blockedCompany.ID) {
		t.Error("blocked company should not be considered active")
	}
	if companyRepo.IsCompanyActive(pendingCompany.ID) {
		t.Error("pending company should not be considered active")
	}
	if companyRepo.IsCompanyActive(99999) {
		t.Error("non-existent company should not be considered active")
	}

	// Create products from different companies
	activeProduct := &model.Product{
		EAN:       "111",
		Name:      "Product from Active",
		CompanyID: activeCompany.ID,
		Price:     100,
		Currency:  "PLN",
	}

	blockedProduct := &model.Product{
		EAN:       "222",
		Name:      "Product from Blocked",
		CompanyID: blockedCompany.ID,
		Price:     200,
		Currency:  "PLN",
	}

	pendingProduct := &model.Product{
		EAN:       "333",
		Name:      "Product from Pending",
		CompanyID: pendingCompany.ID,
		Price:     300,
		Currency:  "PLN",
	}

	for _, p := range []*model.Product{activeProduct, blockedProduct, pendingProduct} {
		if err := productRepo.Create(p); err != nil {
			t.Fatalf("create product: %v", err)
		}
	}

	// Create price documents for each company (source of truth for cleanup).
	for _, comp := range []*model.Company{activeCompany, blockedCompany, pendingCompany} {
		var products []*model.Product
		switch comp.ID {
		case activeCompany.ID:
			products = []*model.Product{activeProduct}
		case blockedCompany.ID:
			products = []*model.Product{blockedProduct}
		case pendingCompany.ID:
			products = []*model.Product{pendingProduct}
		}

		doc := &CompanyPricesDoc{Prices: make(map[int64]float64)}
		for _, p := range products {
			doc.Prices[p.ID] = p.Price
		}
		if err := productRepo.SaveCompanyPrices(comp.ID, doc); err != nil {
			t.Fatalf("save company prices for %d: %v", comp.ID, err)
		}
	}

	// Test cleanup of inactive company products
	inactiveIDs := map[int64]struct{}{
		blockedCompany.ID: {},
		pendingCompany.ID: {},
		activeCompany.ID:  {}, // Should NOT be cleaned up
	}

	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	deleted := productRepo.CleanupInactiveCompanyProductsTx(txn, inactiveIDs, companyRepo.IsCompanyActive)
	if err := txn.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if deleted != 2 {
		t.Errorf("expected 2 products deleted (blocked + pending), got %d", deleted)
	}

	// Verify active product still exists
	if _, err := productRepo.Get(activeProduct.ID); err != nil {
		t.Errorf("active product should still exist: %v", err)
	}

	// Verify inactive company products are gone
	if _, err := productRepo.Get(blockedProduct.ID); err == nil {
		t.Error("blocked company product should be deleted")
	}
	if _, err := productRepo.Get(pendingProduct.ID); err == nil {
		t.Error("pending company product should be deleted")
	}
}
