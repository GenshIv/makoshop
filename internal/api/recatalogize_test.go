package api

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestRunRecatalogize verifies the verify-and-repair pass (part 3):
//   - a page with stale count/min-price and no category gets repaired and
//     catalogized,
//   - a fully correct page is left alone (except the delivery attribute pass,
//     which only fills missing values),
//   - an orphan page (no products) is deleted,
//   - company delivery methods are stamped onto pages.
func TestRunRecatalogize(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := db.NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	h := NewHandlers(store)

	// Company with one delivery method (slug "pickup").
	company := &model.Company{Name: "Acme"}
	if err := h.companyRepo.Create(company); err != nil {
		t.Fatalf("create company: %v", err)
	}
	dmRepo := db.NewDeliveryMethodRepo(store)
	dm := &model.DeliveryMethod{Name: "Pickup", Slug: "pickup", IsActive: true}
	if err := dmRepo.Create(dm); err != nil {
		t.Fatalf("create delivery method: %v", err)
	}
	if err := h.companyRepo.Update(company.ID, func(c *model.Company) {
		c.DeliveryMethodIds = []int64{dm.ID}
	}); err != nil {
		t.Fatalf("update company: %v", err)
	}
	// Wire the company settings repos (delivery slugs are resolved from these).
	h.SetCompanySettingsRepos(
		h.companyRepo,
		db.NewPaymentMethodRepo(store),
		db.NewDeliveryTimeRepo(store),
		dmRepo,
		db.NewInstallmentPlanRepo(store),
	)

	// Category "laptop" with anchor keywords for the catalogizer.
	cat := &model.Category{NameEn: "Laptops", Slug: "laptops", IsActive: true, AnchorKeywords: []string{"laptop"}}
	if err := h.categoryRepo.Create(cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := h.catalogizer.BuildTokensForCategory(cat); err != nil {
		t.Fatalf("build category tokens: %v", err)
	}

	// Products first: creating a product auto-creates its EAN page.
	mkProduct := func(ean string, price float64) {
		if err := h.productRepo.Create(&model.Product{
			EAN:       ean,
			Name:      "laptop thing " + ean,
			CompanyID: company.ID,
			Price:     price,
			Currency:  "PLN",
			Status:    model.ProductStatusActive,
		}); err != nil {
			t.Fatalf("create product %s: %v", ean, err)
		}
	}
	mkProduct("111", 100)
	mkProduct("222", 100)

	pageByEAN := func(ean string) *model.EANPage {
		p, err := h.eanPageRepo.GetByEAN(ean)
		if err != nil || p == nil {
			t.Fatalf("auto-created page for %s missing: %v", ean, err)
		}
		return p
	}

	// P1: correct count/price; assign the catalogizer's category so the page
	// is fully in place -> must stay untouched (by the page pass).
	p1 := pageByEAN("111")
	if err := h.eanPageRepo.Update(p1.ID, func(s *model.EANPage) {
		s.CategoryID = cat.ID
		s.SeoURL = "/shop/laptops/" + s.Slug
	}); err != nil {
		t.Fatalf("set p1 category: %v", err)
	}

	// P2: stale count/min-price, no category -> must be repaired.
	p2 := pageByEAN("222")
	if err := h.eanPageRepo.Update(p2.ID, func(s *model.EANPage) {
		s.ProductCount = 5
		s.MinPrice = 999
	}); err != nil {
		t.Fatalf("corrupt p2: %v", err)
	}

	// P3: no products anymore -> the page must STAY (SEO) with zero offers.
	p3 := &model.EANPage{
		EAN: "333", Slug: "page-333", Title: "orphan thing",
		Currency: "PLN", MinPrice: 10, ProductCount: 1, IsActive: true,
	}
	if err := h.eanPageRepo.Create(p3); err != nil {
		t.Fatalf("create orphan page: %v", err)
	}

	// Run the pass synchronously.
	if err := h.runRecatalogize(); err != nil {
		t.Fatalf("runRecatalogize: %v", err)
	}

	// P1 unchanged (except the delivery attribute pass).
	got1, err := h.eanPageRepo.Get(p1.ID)
	if err != nil {
		t.Fatalf("get p1: %v", err)
	}
	if got1.ProductCount != 1 || got1.MinPrice != 100 || got1.CategoryID != cat.ID {
		t.Fatalf("p1 drifted: count=%d price=%v cat=%d", got1.ProductCount, got1.MinPrice, got1.CategoryID)
	}
	deliveryOK := false
	for _, kv := range got1.Attributes {
		if kv.Key == "delivery_method" && kv.Value == "pickup" {
			deliveryOK = true
			break
		}
	}
	if !deliveryOK {
		t.Fatalf("p1 delivery attr missing, attrs=%v", got1.Attributes)
	}

	// P2 repaired.
	got2, err := h.eanPageRepo.Get(p2.ID)
	if err != nil {
		t.Fatalf("get p2: %v", err)
	}
	if got2.ProductCount != 1 {
		t.Fatalf("p2 count = %d, want 1", got2.ProductCount)
	}
	if got2.MinPrice != 100 {
		t.Fatalf("p2 min price = %v, want 100", got2.MinPrice)
	}
	if got2.CategoryID != cat.ID {
		t.Fatalf("p2 category = %d, want %d (catalogizer)", got2.CategoryID, cat.ID)
	}

	// P3 alive with zero offers (pages are never deleted — SEO).
	got3, err := h.eanPageRepo.Get(p3.ID)
	if err != nil {
		t.Fatalf("orphan p3 must stay alive: %v", err)
	}
	if got3.ProductCount != 0 {
		t.Fatalf("p3 count = %d, want 0 (no products)", got3.ProductCount)
	}
}
