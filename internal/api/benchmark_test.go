package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

var testHandlers *Handlers

func TestMain(m *testing.M) {
	// Use test DB or existing DB from env
	dbPath := os.Getenv("TEST_DB_PATH")
	if dbPath == "" {
		dbPath = "./_tmp/test_bench.db"
	}

	cfg := config.DefaultConfig()
	cfg.Database.Path = dbPath

	store, err := db.NewStore(cfg.Database)
	if err != nil {
		panic("failed to open database for benchmarks: " + err.Error())
	}
	defer store.Close()

	testHandlers = NewHandlers(store)
	code := m.Run()
	os.Exit(code)
}

// BenchmarkShopRoot — бенчмарк корневого каталога
func BenchmarkShopRoot(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopRootHEAD — корневой каталог (HEAD)
func BenchmarkShopRootHEAD(b *testing.B) {
	req := httptest.NewRequest(http.MethodHead, "/shop", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopRootJSON — корневой каталог (JSON)
func BenchmarkShopRootJSON(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop?limit=50", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopCategory — каталог категории
func BenchmarkShopCategory(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop/elektronika", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopCategoryDeep — каталог вложенной категории
func BenchmarkShopCategoryDeep(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop/elektronika/telefony", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopCategoryWithFilters — каталог с фильтрами
func BenchmarkShopCategoryWithFilters(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop/elektronika?price_min=1000&price_max=100000&sort=price_asc&limit=50", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopSlug — страница SCUPage по slug
func BenchmarkShopSlug(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop/elektronika/telefony/samsung-galaxy-s7", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkShopSlugWithHTML — страница SCUPage с HTML (SSR)
func BenchmarkShopSlugWithHTML(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/shop/elektronika/telefony/samsung-galaxy-s7", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Googlebot/2.1")
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSCUPageByPath(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkCategoriesTree — дерево категорий
func BenchmarkCategoriesTree(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/categories/tree", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleCategoriesTree(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkProductsTurbo — turbo-поиск
func BenchmarkProductsTurbo(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/products/turbo?q=телефон&limit=20", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleTurboProducts(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkProductsTurboHEAD — turbo-поиск (HEAD)
func BenchmarkProductsTurboHEAD(b *testing.B) {
	req := httptest.NewRequest(http.MethodHead, "/products/turbo?q=телефон&limit=20", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleTurboProducts(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkProductsTurboWithFilters — turbo-поиск с фильтрами
func BenchmarkProductsTurboWithFilters(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/products/turbo?q=телефон&sort=price_asc&limit=50", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleTurboProducts(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkProductsList — список продуктов
func BenchmarkProductsList(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/products?limit=50", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleProductsList(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkAttributeValues — значения атрибута
func BenchmarkAttributeValues(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/attributes/brand/values", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleAttributeValues(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkSitemapIndex — sitemap index
func BenchmarkSitemapIndex(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSitemapIndex(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// BenchmarkSitemapSCUPage — sitemap SCUPage
func BenchmarkSitemapSCUPage(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/sitemap-scupage-1.xml", nil)
	w := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testHandlers.HandleSitemapSCUPage(w, req)
		_ = w.Result()
		w = httptest.NewRecorder()
	}
}

// TestConcurrentRequests — быстрый тест конкурентных запросов (race detection).
// Запускается с -race для поиска data races.
func TestConcurrentRequests(t *testing.T) {
	const goroutines = 50
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				switch id % 4 {
				case 0:
					req := httptest.NewRequest(http.MethodGet, "/shop", nil)
					w := httptest.NewRecorder()
					testHandlers.HandleSCUPageByPath(w, req)
				case 1:
					req := httptest.NewRequest(http.MethodGet, "/shop/elektronika", nil)
					w := httptest.NewRecorder()
					testHandlers.HandleSCUPageByPath(w, req)
				case 2:
					req := httptest.NewRequest(http.MethodGet, "/shop/elektronika/telefony/samsung-galaxy-s7", nil)
					w := httptest.NewRecorder()
					testHandlers.HandleSCUPageByPath(w, req)
				case 3:
					req := httptest.NewRequest(http.MethodGet, "/categories/tree", nil)
					w := httptest.NewRecorder()
					testHandlers.HandleCategoriesTree(w, req)
				}
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}
