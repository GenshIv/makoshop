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
	"sync"
	"sync/atomic"
	"time"
)

// bench_server_profile.go — бенчмарк с профилированием СЕРВЕРА через /debug/pprof.
//
// Использование:
//   cd loadtest && go run bench_server_profile.go -c 200 -d 30s
//
// Профилирует сервер (не клиент) через встроенные pprof эндпоинты.

type EndpointStats struct {
	Name       string
	Count      int64
	Errors     int64
	TotalTime  time.Duration
	MinLatency time.Duration
	MaxLatency time.Duration
	Latencies  []time.Duration
}

func (s *EndpointStats) Add(latency time.Duration, status int) {
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
	if len(s.Latencies) < 5000 {
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

func main() {
	baseURL := flag.String("url", "http://localhost:9090", "Base URL")
	concurrency := flag.Int("c", 200, "Concurrency")
	duration := flag.Duration("d", 30*time.Second, "Test duration")
	pprofDir := flag.String("pprof-dir", "./pprof_server", "Directory for server pprof files")
	flag.Parse()

	os.MkdirAll(*pprofDir, 0755)

	// Load sample data
	slugs, _ := loadSlugs("./slugs.txt")
	categories, _ := loadSlugs("./categories.txt")

	fmt.Println("=== Server Benchmark with Profiling ===")
	fmt.Printf("URL:         %s\n", *baseURL)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Duration:    %s\n", *duration)
	fmt.Printf("Slugs:       %d\n", len(slugs))
	fmt.Printf("Categories:  %d\n", len(categories))
	fmt.Println()

	// Check if server is up
	resp, err := http.Get(*baseURL + "/health")
	if err != nil {
		fmt.Println("ERROR: Server not reachable:", err)
		os.Exit(1)
	}
	resp.Body.Close()
	fmt.Println("Server is up")

	// Start server CPU profiling via /debug/pprof/profile
	fmt.Println("Starting server CPU profiling (10s)...")
	cpuPath := filepath.Join(*pprofDir, "server_cpu.prof")
	cpuFile, _ := os.Create(cpuPath)
	defer cpuFile.Close()

	// We'll fetch the profile at the end
	// First, trigger a profile request that runs for the duration
	go func() {
		time.Sleep(2 * time.Second) // let load start
		resp, err := http.Get(*baseURL + "/debug/pprof/profile?seconds=25")
		if err != nil {
			fmt.Println("CPU profile error:", err)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(cpuFile, resp.Body)
		fmt.Println("Server CPU profile saved:", cpuPath)
	}()

	// Start load
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			MaxConnsPerHost:     *concurrency * 2,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
	}

	var stats = map[string]*EndpointStats{
		"GET /shop":            {},
		"GET /shop/{category}": {},
		"GET /shop/{slug}":     {},
		"GET /products":        {},
		"GET /products/turbo":  {},
		"GET /categories/tree": {},
	}

	var wg sync.WaitGroup
	start := time.Now()
	done := make(chan struct{})
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				executeRequest(client, *baseURL, rng, slugs, categories, stats)
			}
		}()
	}

	// Progress
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

	// Other profiles (snapshot at end)
	time.Sleep(*duration)
	close(done)
	wg.Wait()
	elapsed := time.Since(start)

	// Fetch heap profile
	fmt.Println("\nFetching server heap profile...")
	heapPath := filepath.Join(*pprofDir, "server_heap.prof")
	if resp, err := http.Get(*baseURL + "/debug/pprof/heap"); err == nil {
		f, _ := os.Create(heapPath)
		_, _ = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		fmt.Println("Heap profile saved:", heapPath)
	}

	// Fetch goroutine profile
	fmt.Println("Fetching server goroutine profile...")
	goroutinePath := filepath.Join(*pprofDir, "server_goroutine.prof")
	if resp, err := http.Get(*baseURL + "/debug/pprof/goroutine?debug=0"); err == nil {
		f, _ := os.Create(goroutinePath)
		_, _ = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		fmt.Println("Goroutine profile saved:", goroutinePath)
	}

	// Fetch mutex profile
	fmt.Println("Fetching server mutex profile...")
	mutexPath := filepath.Join(*pprofDir, "server_mutex.prof")
	if resp, err := http.Get(*baseURL + "/debug/pprof/mutex"); err == nil {
		f, _ := os.Create(mutexPath)
		_, _ = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		fmt.Println("Mutex profile saved:", mutexPath)
	}

	// Fetch block profile
	fmt.Println("Fetching server block profile...")
	blockPath := filepath.Join(*pprofDir, "server_block.prof")
	if resp, err := http.Get(*baseURL + "/debug/pprof/block"); err == nil {
		f, _ := os.Create(blockPath)
		_, _ = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		fmt.Println("Block profile saved:", blockPath)
	}

	// Results
	fmt.Println()
	fmt.Println("=== Benchmark Results ===")
	fmt.Printf("Duration:    %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Concurrency: %d\n\n", *concurrency)

	totalReqs := int64(0)
	totalErrs := int64(0)

	for _, name := range []string{
		"GET /shop",
		"GET /shop/{category}",
		"GET /shop/{slug}",
		"GET /products",
		"GET /products/turbo",
		"GET /categories/tree",
	} {
		s := stats[name]
		if s.Count > 0 {
			printStats(name, s, elapsed)
			totalReqs += s.Count
			totalErrs += s.Errors
		}
	}

	totalErrRate := float64(totalErrs) / float64(totalReqs) * 100
	totalRPS := float64(totalReqs) / elapsed.Seconds()
	fmt.Printf("\nTotal: %d requests, %d errors (%.2f%%), %.0f req/s\n",
		totalReqs, totalErrs, totalErrRate, totalRPS)

	fmt.Println()
	fmt.Println("=== Server Profiles ===")
	fmt.Printf("CPU profile:     %s\n", cpuPath)
	fmt.Printf("Heap profile:    %s\n", heapPath)
	fmt.Printf("Goroutine:       %s\n", goroutinePath)
	fmt.Printf("Mutex profile:   %s\n", mutexPath)
	fmt.Printf("Block profile:   %s\n", blockPath)
	fmt.Println()
	fmt.Println("Analyze with:")
	fmt.Printf("  go tool pprof -top %s\n", cpuPath)
	fmt.Printf("  go tool pprof -top %s\n", heapPath)
	fmt.Printf("  go tool pprof -top %s\n", mutexPath)
}

func executeRequest(client *http.Client, baseURL string, rng *rand.Rand,
	slugs, categories []string, stats map[string]*EndpointStats) {

	r := rng.Float64()
	var stat *EndpointStats
	var url string

	if r < 0.30 {
		stat = stats["GET /shop"]
		url = baseURL + "/shop?limit=50"
	} else if r < 0.50 && len(categories) > 0 {
		stat = stats["GET /shop/{category}"]
		cat := categories[rng.Intn(len(categories))]
		url = baseURL + "/shop/" + cat + "?limit=50"
	} else if r < 0.75 && len(slugs) > 0 {
		stat = stats["GET /shop/{slug}"]
		slug := slugs[rng.Intn(len(slugs))]
		url = baseURL + "/shop/" + slug
	} else if r < 0.83 {
		stat = stats["GET /products"]
		url = baseURL + "/products?limit=20"
	} else if r < 0.90 {
		stat = stats["GET /products/turbo"]
		queries := []string{"телефон", "ноутбук", "наушники"}
		q := queries[rng.Intn(len(queries))]
		url = baseURL + "/products/turbo?q=" + q + "&limit=20"
	} else {
		stat = stats["GET /categories/tree"]
		url = baseURL + "/categories/tree"
	}

	start := time.Now()
	resp, err := client.Get(url)
	latency := time.Since(start)

	if err != nil {
		stat.Add(latency, 0)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	stat.Add(latency, resp.StatusCode)
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
