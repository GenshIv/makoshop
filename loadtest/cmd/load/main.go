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
	"sync"
	"sync/atomic"
	"time"
)

type Result struct {
	Endpoint string
	Latency  time.Duration
	Status   int
}

type Stats struct {
	Count      int64
	Errors     int64
	TotalTime  time.Duration
	MinLatency time.Duration
	MaxLatency time.Duration
}

func (s *Stats) Add(latency time.Duration, status int) {
	s.Count++
	s.TotalTime += latency
	if status >= 400 {
		s.Errors++
	}
	if s.MinLatency == 0 || latency < s.MinLatency {
		s.MinLatency = latency
	}
	if latency > s.MaxLatency {
		s.MaxLatency = latency
	}
}

func (s *Stats) Avg() time.Duration {
	if s.Count == 0 {
		return 0
	}
	return s.TotalTime / time.Duration(s.Count)
}

func main() {
	baseURL := flag.String("url", "http://localhost:9090", "Base URL")
	concurrency := flag.Int("c", 50, "Concurrency")
	duration := flag.Duration("d", 30*time.Second, "Test duration")
	flag.Parse()

	// Load sample slugs for realistic requests
	slugs, err := loadSlugs("./slugs.txt")
	if err != nil {
		fmt.Println("Warning: could not load slugs.txt, will use /shop only:", err)
	}

	// Load sample categories for realistic requests
	categories, err := loadSlugs("./categories.txt")
	if err != nil {
		fmt.Println("Warning: could not load categories.txt, will use /shop only:", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			MaxConnsPerHost:     1000,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	var (
		statsShop          Stats
		statsShopCat       Stats
		statsShopSlug      Stats
		statsProducts      Stats
		statsCategories    Stats
		statsProductsTurbo Stats
	)

	start := time.Now()
	done := make(chan struct{})

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(idx)))
			for {
				select {
				case <-done:
					return
				default:
				}

				// Random endpoint selection (weighted towards /shop)
				r := rng.Float64()
				var stat *Stats
				var url string
				var start time.Time

				if r < 0.35 {
					// /shop root
					stat = &statsShop
					url = *baseURL + "/shop"
				} else if r < 0.55 && len(categories) > 0 {
					// /shop/{category}
					stat = &statsShopCat
					cat := categories[rng.Intn(len(categories))]
					url = *baseURL + "/shop/" + cat
				} else if r < 0.80 && len(slugs) > 0 {
					// /shop/{slug}
					stat = &statsShopSlug
					slug := slugs[rng.Intn(len(slugs))]
					url = *baseURL + "/shop/" + slug
				} else if r < 0.88 {
					// /products
					stat = &statsProducts
					url = *baseURL + "/products?limit=20"
				} else if r < 0.93 {
					// /products/turbo
					stat = &statsProductsTurbo
					url = *baseURL + "/products/turbo?q=телефон&limit=20"
				} else {
					// /categories/tree
					stat = &statsCategories
					url = *baseURL + "/categories/tree"
				}

				start = time.Now()
				resp, err := client.Get(url)
				latency := time.Since(start)

				if err != nil {
					stat.Add(latency, 0)
					continue
				}
				// Drain body to reuse connection, but limit to 64KB to avoid memory pressure
				_, _ = io.CopyN(io.Discard, resp.Body, 64*1024)
				resp.Body.Close()
				stat.Add(latency, resp.StatusCode)
			}
		}(i)
	}

	// Progress reporting
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			elapsed := time.Since(start)
			fmt.Printf("\r[%s] Running... shop:%d shopCat:%d shopSlug:%d products:%d turbo:%d cats:%d",
				elapsed.Round(time.Second),
				atomic.LoadInt64(&statsShop.Count),
				atomic.LoadInt64(&statsShopCat.Count),
				atomic.LoadInt64(&statsShopSlug.Count),
				atomic.LoadInt64(&statsProducts.Count),
				atomic.LoadInt64(&statsProductsTurbo.Count),
				atomic.LoadInt64(&statsCategories.Count),
			)
		}
	}()

	// Stop after duration
	time.Sleep(*duration)
	close(done)
	wg.Wait()
	elapsed := time.Since(start)

	fmt.Println()
	fmt.Println("=== Load Test Results ===")
	fmt.Printf("Duration:   %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Concurrency: %d\n\n", *concurrency)

	printStats("GET /shop", &statsShop, elapsed)
	printStats("GET /shop/{category}", &statsShopCat, elapsed)
	printStats("GET /shop/{slug}", &statsShopSlug, elapsed)
	printStats("GET /products", &statsProducts, elapsed)
	printStats("GET /products/turbo", &statsProductsTurbo, elapsed)
	printStats("GET /categories/tree", &statsCategories, elapsed)

	// Summary
	total := statsShop.Count + statsShopCat.Count + statsShopSlug.Count +
		statsProducts.Count + statsProductsTurbo.Count + statsCategories.Count
	totalErrors := statsShop.Errors + statsShopCat.Errors + statsShopSlug.Errors +
		statsProducts.Errors + statsProductsTurbo.Errors + statsCategories.Errors
	fmt.Printf("\nTotal: %d requests, %d errors (%.2f%%), %.0f req/s\n",
		total, totalErrors,
		float64(totalErrors)/float64(total)*100,
		float64(total)/elapsed.Seconds())
}

func printStats(name string, s *Stats, elapsed time.Duration) {
	rps := float64(s.Count) / elapsed.Seconds()
	errRate := float64(s.Errors) / float64(s.Count) * 100
	fmt.Printf("%-25s %6d reqs  %.0f req/s  avg=%-8s min=%-8s max=%-8s err=%.1f%%\n",
		name, s.Count, rps,
		s.Avg().Round(time.Millisecond),
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
