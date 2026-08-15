package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"
)

// bench.go — бенчмарк с профилированием CPU, memory, mutex, block.
//
// Использование:
//   cd loadtest && go run bench.go -c 100 -d 60s -pprof
//
// Профилирование:
//   -pprof           — включить профилирование (cpu, mem, mutex, block)
//   -pprof-dir DIR   — директория для профилей (default: ./pprof_out)
//   -cpu-profile     — только CPU профиль
//   -mem-profile     — только memory профиль
//   -mutex-profile   — только mutex профиль
//   -block-profile   — только block профиль

type EndpointStats struct {
	Name       string
	Count      int64
	Errors     int64
	TotalTime  time.Duration
	MinLatency time.Duration
	MaxLatency time.Duration
	// Latency buckets
	Latencies []time.Duration
}

func (s *EndpointStats) Add(latency time.Duration, status int, recordLatency bool) {
	s.Count++
	s.TotalTime += latency
	if status >= 400 || status == 0 {
		s.Errors++
	}
	if s.MinLatency == 0 || latency < s.MinLatency {
		s.MinLatency = latency
	}
	if latency > s.MaxLatency {
		s.MaxLatency = latency
	}
	if recordLatency && len(s.Latencies) < 10000 {
		s.Latencies = append(s.Latencies, latency)
	}
}

func (s *EndpointStats) Avg() time.Duration {
	if s.Count == 0 {
		return 0
	}
	return s.TotalTime / time.Duration(s.Count)
}

func (s *EndpointStats) P95() time.Duration {
	if len(s.Latencies) == 0 {
		return s.Avg()
	}
	latencies := make([]time.Duration, len(s.Latencies))
	copy(latencies, s.Latencies)
	n := len(latencies)
	_ = quickSelect(latencies, int(float64(n)*0.95))
	return latencies[int(float64(n)*0.95)]
}

func (s *EndpointStats) P99() time.Duration {
	if len(s.Latencies) == 0 {
		return s.Avg()
	}
	latencies := make([]time.Duration, len(s.Latencies))
	copy(latencies, s.Latencies)
	n := len(latencies)
	_ = quickSelect(latencies, int(float64(n)*0.99))
	return latencies[int(float64(n)*0.99)]
}

// quickSelect — partial sort for percentile calculation
func quickSelect(a []time.Duration, k int) int {
	if len(a) <= 1 {
		return 0
	}
	pivot := a[len(a)/2]
	left, right := 0, len(a)-1
	for left <= right {
		for a[left] < pivot {
			left++
		}
		for a[right] > pivot {
			right--
		}
		if left <= right {
			a[left], a[right] = a[right], a[left]
			left++
			right--
		}
	}
	if k <= right {
		return quickSelect(a[:right+1], k)
	}
	if k >= left {
		return quickSelect(a[left:], k-left)
	}
	return k
}

type Config struct {
	BaseURL       string
	Concurrency   int
	Duration      time.Duration
	PPROF         bool
	PPROFDir      string
	CPUProfile    bool
	MemProfile    bool
	MutexProfile  bool
	BlockProfile  bool
	RecordLatency bool
}

func main() {
	cfg := &Config{}

	flag.StringVar(&cfg.BaseURL, "url", "http://localhost:9090", "Base URL")
	flag.IntVar(&cfg.Concurrency, "c", 100, "Concurrency")
	flag.DurationVar(&cfg.Duration, "d", 60*time.Second, "Test duration")
	flag.BoolVar(&cfg.PPROF, "pprof", true, "Enable profiling (all types)")
	flag.StringVar(&cfg.PPROFDir, "pprof-dir", "./pprof_out", "Directory for pprof files")
	flag.BoolVar(&cfg.CPUProfile, "cpu-profile", false, "CPU profile only")
	flag.BoolVar(&cfg.MemProfile, "mem-profile", false, "Memory profile only")
	flag.BoolVar(&cfg.MutexProfile, "mutex-profile", false, "Mutex profile only")
	flag.BoolVar(&cfg.BlockProfile, "block-profile", false, "Block profile only")
	flag.BoolVar(&cfg.RecordLatency, "latencies", true, "Record individual latencies for percentiles")
	flag.Parse()

	// If specific profile flags are set, disable general -pprof
	if cfg.CPUProfile || cfg.MemProfile || cfg.MutexProfile || cfg.BlockProfile {
		cfg.PPROF = false
	}

	// Create output directory
	os.MkdirAll(cfg.PPROFDir, 0755)

	// Load sample data
	slugs, err := loadSlugs("./slugs.txt")
	if err != nil {
		fmt.Println("Warning: could not load slugs.txt:", err)
	}

	categories, err := loadSlugs("./categories.txt")
	if err != nil {
		fmt.Println("Warning: could not load categories.txt:", err)
	}

	fmt.Println("=== Benchmark Configuration ===")
	fmt.Printf("URL:         %s\n", cfg.BaseURL)
	fmt.Printf("Concurrency: %d\n", cfg.Concurrency)
	fmt.Printf("Duration:    %s\n", cfg.Duration)
	fmt.Printf("Slugs:       %d loaded\n", len(slugs))
	fmt.Printf("Categories:  %d loaded\n", len(categories))
	fmt.Printf("Profiling:   pprof=%v cpu=%v mem=%v mutex=%v block=%v\n",
		cfg.PPROF, cfg.CPUProfile, cfg.MemProfile, cfg.MutexProfile, cfg.BlockProfile)
	fmt.Println()

	// Initialize HTTP client
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        cfg.Concurrency * 2,
			MaxIdleConnsPerHost: cfg.Concurrency * 2,
			MaxConnsPerHost:     cfg.Concurrency * 2,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
	}

	// Stats per endpoint
	var stats = map[string]*EndpointStats{
		"GET /shop":              {},
		"GET /shop/{category}":   {},
		"GET /shop/{slug}":       {},
		"GET /products":          {},
		"GET /products/turbo":    {},
		"GET /categories/tree":   {},
		"GET /attributes/{code}": {},
		"GET /sitemap.xml":       {},
	}

	// Start profiling
	var cpuFile *os.File
	if cfg.PPROF || cfg.CPUProfile {
		cpuPath := filepath.Join(cfg.PPROFDir, "cpu.prof")
		cpuFile, _ = os.Create(cpuPath)
		_ = pprof.StartCPUProfile(cpuFile)
		fmt.Println("CPU profiling started:", cpuPath)
	}

	if cfg.PPROF || cfg.MutexProfile {
		runtime.SetMutexProfileFraction(1)
	}
	if cfg.PPROF || cfg.BlockProfile {
		runtime.SetBlockProfileRate(1)
	}

	// Run benchmark
	var wg sync.WaitGroup
	start := time.Now()
	done := make(chan struct{})

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}

				executeRequest(client, cfg, rng, slugs, categories, stats, cfg.RecordLatency)
			}
		}()
	}

	// Progress reporting
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			elapsed := time.Since(start)
			totalReqs := int64(0)
			totalErrs := int64(0)
			for _, s := range stats {
				totalReqs += atomic.LoadInt64(&s.Count)
				totalErrs += atomic.LoadInt64(&s.Errors)
			}
			rps := float64(totalReqs) / elapsed.Seconds()
			fmt.Printf("\r[%s] %d reqs, %d errs, %.0f req/s",
				elapsed.Round(time.Second), totalReqs, totalErrs, rps)
		}
	}()

	// Stop after duration
	time.Sleep(cfg.Duration)
	close(done)
	wg.Wait()
	elapsed := time.Since(start)

	// Stop profiling
	if cpuFile != nil {
		pprof.StopCPUProfile()
		cpuFile.Close()
		fmt.Println("\nCPU profiling stopped")
	}

	// Write memory profile
	if cfg.PPROF || cfg.MemProfile {
		memPath := filepath.Join(cfg.PPROFDir, "mem.prof")
		memFile, _ := os.Create(memPath)
		_ = pprof.WriteHeapProfile(memFile)
		memFile.Close()
		fmt.Println("Memory profiling done:", memPath)
	}

	// Write mutex profile
	if cfg.PPROF || cfg.MutexProfile {
		mutexPath := filepath.Join(cfg.PPROFDir, "mutex.prof")
		mutexFile, _ := os.Create(mutexPath)
		mutexProfile := pprof.Lookup("mutex")
		if mutexProfile != nil {
			_ = mutexProfile.WriteTo(mutexFile, 0)
		}
		mutexFile.Close()
		fmt.Println("Mutex profiling done:", mutexPath)
	}

	// Write block profile
	if cfg.PPROF || cfg.BlockProfile {
		blockPath := filepath.Join(cfg.PPROFDir, "block.prof")
		blockFile, _ := os.Create(blockPath)
		blockProfile := pprof.Lookup("block")
		if blockProfile != nil {
			_ = blockProfile.WriteTo(blockFile, 0)
		}
		blockFile.Close()
		fmt.Println("Block profiling done:", blockPath)
	}

	// Print results
	fmt.Println()
	fmt.Println("=== Benchmark Results ===")
	fmt.Printf("Duration:    %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Concurrency: %d\n\n", cfg.Concurrency)

	totalReqs := int64(0)
	totalErrs := int64(0)

	for _, name := range []string{
		"GET /shop",
		"GET /shop/{category}",
		"GET /shop/{slug}",
		"GET /products",
		"GET /products/turbo",
		"GET /categories/tree",
		"GET /attributes/{code}",
		"GET /sitemap.xml",
	} {
		s := stats[name]
		if s.Count > 0 {
			printStats(name, s, elapsed)
			totalReqs += s.Count
			totalErrs += s.Errors
		}
	}

	// Summary
	totalErrRate := float64(totalErrs) / float64(totalReqs) * 100
	totalRPS := float64(totalReqs) / elapsed.Seconds()
	fmt.Printf("\nTotal: %d requests, %d errors (%.2f%%), %.0f req/s\n",
		totalReqs, totalErrs, totalErrRate, totalRPS)
}

func executeRequest(client *http.Client, cfg *Config, rng *rand.Rand,
	slugs, categories []string, stats map[string]*EndpointStats, recordLatency bool) {

	r := rng.Float64()
	var stat *EndpointStats
	var url string

	// Weighted endpoint selection (reflects real traffic patterns)
	if r < 0.30 {
		// /shop root catalog
		stat = stats["GET /shop"]
		url = cfg.BaseURL + "/shop?limit=50"
	} else if r < 0.50 && len(categories) > 0 {
		// /shop/{category}
		stat = stats["GET /shop/{category}"]
		cat := categories[rng.Intn(len(categories))]
		url = cfg.BaseURL + "/shop/" + cat + "?limit=50"
	} else if r < 0.75 && len(slugs) > 0 {
		// /shop/{slug} — product page
		stat = stats["GET /shop/{slug}"]
		slug := slugs[rng.Intn(len(slugs))]
		url = cfg.BaseURL + "/shop/" + slug
	} else if r < 0.83 {
		// /products
		stat = stats["GET /products"]
		url = cfg.BaseURL + "/products?limit=20"
	} else if r < 0.90 {
		// /products/turbo
		stat = stats["GET /products/turbo"]
		queries := []string{"телефон", "ноутбук", "наушники", "планшет", "смартфон"}
		q := queries[rng.Intn(len(queries))]
		url = cfg.BaseURL + "/products/turbo?q=" + q + "&limit=20"
	} else if r < 0.95 {
		// /categories/tree
		stat = stats["GET /categories/tree"]
		url = cfg.BaseURL + "/categories/tree"
	} else if r < 0.98 {
		// /attributes/{code}/values
		stat = stats["GET /attributes/{code}"]
		codes := []string{"brand", "color", "screen_size"}
		code := codes[rng.Intn(len(codes))]
		url = cfg.BaseURL + "/attributes/" + code + "/values"
	} else {
		// /sitemap.xml
		stat = stats["GET /sitemap.xml"]
		url = cfg.BaseURL + "/sitemap.xml"
	}

	start := time.Now()
	resp, err := client.Get(url)
	latency := time.Since(start)

	if err != nil {
		stat.Add(latency, 0, recordLatency)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	stat.Add(latency, resp.StatusCode, recordLatency)
}

func printStats(name string, s *EndpointStats, elapsed time.Duration) {
	rps := float64(s.Count) / elapsed.Seconds()
	errRate := float64(s.Errors) / float64(s.Count) * 100
	fmt.Printf("%-25s %6d reqs  %.0f req/s  avg=%-8s p95=%-8s p99=%-8s min=%-8s max=%-8s err=%.1f%%\n",
		name, s.Count, rps,
		s.Avg().Round(time.Millisecond),
		s.P95().Round(time.Millisecond),
		s.P99().Round(time.Millisecond),
		s.MinLatency.Round(time.Millisecond),
		s.MaxLatency.Round(time.Millisecond),
		errRate)
}

func loadSlugs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}
