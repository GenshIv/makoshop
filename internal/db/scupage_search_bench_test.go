package db

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"

	"github.com/GenshIv/makoshop/pkg/config"
)

// TestBenchListWithTurbo — CPU + memory профилирование ListWithTurbo на реальном запросе.
//
// Запуск:
//
//	cd /home/ihar/IdeaProjects/makoshop/internal/db
//	go test -run=^TestBenchListWithTurbo$ -v -timeout=120s
//
// Профили записываются в ./pprof_out/
func TestBenchListWithTurbo(t *testing.T) {
	// Создаем директорию для профилей
	pprofDir := "./pprof_out"
	os.MkdirAll(pprofDir, 0755)

	// Открываем существующую БД makoshop
	cfg := config.DatabaseConfig{
		Path:               "/home/ihar/IdeaProjects/makoshop/makoshop_db",
		NumShards:          16,
		MaxTotalSize:       40 * 1024 * 1024 * 1024,
		NumBucketsPerShard: 5_000_000,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	repo := NewEANPageRepo(store)
	categoryRepo := NewCategoryRepo(store)
	promoCampaignRepo := NewPromoCampaignRepo(store)
	promoPlanRepo := NewPromoPlanRepo(store)
	promoLogRepo := NewPromoLogRepo(store)
	productRepo := NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)

	search := NewEANPageSearch(store.DB(), repo, productRepo, categoryRepo, true)

	// Запускаем профилирование

	// CPU profile
	cpuPath := filepath.Join(pprofDir, "eanpage_list_cpu.prof")
	cpuFile, err := os.Create(cpuPath)
	if err != nil {
		t.Fatalf("create cpu profile: %v", err)
	}
	pprof.StartCPUProfile(cpuFile)

	// Mutex + block profiles
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)

	// Запускаем бенчмарк
	t.Run("list_with_turbo", func(t *testing.T) {
		benchListWithTurbo(t, search)
	})

	// Останавливаем профилирование
	pprof.StopCPUProfile()
	cpuFile.Close()

	// Memory profile
	memPath := filepath.Join(pprofDir, "eanpage_list_mem.prof")
	memFile, err := os.Create(memPath)
	if err != nil {
		t.Fatalf("create mem profile: %v", err)
	}
	runtime.GC() // force GC for accurate heap profile
	pprof.WriteHeapProfile(memFile)
	memFile.Close()

	// Mutex profile
	mutexPath := filepath.Join(pprofDir, "eanpage_list_mutex.prof")
	mutexFile, err := os.Create(mutexPath)
	if err != nil {
		t.Fatalf("create mutex profile: %v", err)
	}
	mutexProfile := pprof.Lookup("mutex")
	if mutexProfile != nil {
		mutexProfile.WriteTo(mutexFile, 0)
	}
	mutexFile.Close()

	// Block profile
	blockPath := filepath.Join(pprofDir, "eanpage_list_block.prof")
	blockFile, err := os.Create(blockPath)
	if err != nil {
		t.Fatalf("create block profile: %v", err)
	}
	blockProfile := pprof.Lookup("block")
	if blockProfile != nil {
		blockProfile.WriteTo(blockFile, 0)
	}
	blockFile.Close()

	fmt.Printf("\nProfiles written to %s:\n", pprofDir)
	fmt.Printf("  CPU:   %s\n", cpuPath)
	fmt.Printf("  MEM:   %s\n", memPath)
	fmt.Printf("  Mutex: %s\n", mutexPath)
	fmt.Printf("  Block: %s\n", blockPath)
	fmt.Printf("\nView with: go tool pprof -http=:8080 %s\n", cpuPath)
}

func benchListWithTurbo(t *testing.T, search *EANPageSearch) {
	// Сначала узнаем, какие категории есть
	cats, err := search.categoryRepo.ListAll()
	if err != nil {
		t.Logf("List categories error: %v", err)
	} else {
		t.Logf("Categories count: %d", len(cats))
		if len(cats) > 0 {
			for i, c := range cats {
				if i >= 10 {
					break
				}
				t.Logf("  Cat %d: %s (parent=%v)", c.ID, c.NameRu, c.ParentID)
			}
		}
	}

	// Пытаемся разные варианты запросов
	testCases := []struct {
		name   string
		params EANPageListParams
	}{
		{"no_filters", EANPageListParams{Sort: "price_asc", Page: 1, Limit: 50}},
		{"text_only", EANPageListParams{Q: "телефон", Sort: "price_asc", Page: 1, Limit: 50}},
		{"cat_1", EANPageListParams{CategoryID: 1, Sort: "price_asc", Page: 1, Limit: 50}},
		{"cat_1_text", EANPageListParams{CategoryID: 1, Q: "телефон", Sort: "price_asc", Page: 1, Limit: 50}},
	}

	for _, tc := range testCases {
		res, err := search.ListWithTurbo(tc.params)
		if err != nil {
			t.Logf("%s error: %v", tc.name, err)
		} else {
			t.Logf("%s: total=%d items=%d", tc.name, res.Total, len(res.Items))
		}
	}

	// Если есть результаты — запускаем бенчмарк на самом быстром запросе
	var bestParams EANPageListParams
	var bestTotal int64 = -1

	for _, tc := range testCases {
		res, err := search.ListWithTurbo(tc.params)
		if err == nil && res.Total > bestTotal {
			bestTotal = res.Total
			bestParams = tc.params
		}
	}

	if bestTotal <= 0 {
		t.Log("No results found in any test case, skipping benchmark")
		return
	}

	t.Logf("Benchmarking with params: %+v (total=%d)", bestParams, bestTotal)

	// Замер: 50 запросов
	const iterations = 50
	for i := 0; i < iterations; i++ {
		res, err := search.ListWithTurbo(bestParams)
		if err != nil {
			t.Logf("ListWithTurbo #%d error: %v", i, err)
			continue
		}
		t.Logf("ListWithTurbo #%d: total=%d items=%d", i, res.Total, len(res.Items))
	}
}
