//go:build ignore

// profile_scupage.go — профилирование ListWithTurbo через HTTP-запросы к работающему серверу.
//
// Использование:
//
//	cd /home/ihar/IdeaProjects/makoshop/loadtest/cmd
//	go run profile_scupage.go -url http://localhost:9090 -iterations 100
//
// Профили записываются в ./pprof_out/
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func main() {
	var (
		baseURL    string
		iterations int
		conc       int
		pprofDir   string
	)

	flag.StringVar(&baseURL, "url", "http://localhost:9090", "Base URL")
	flag.IntVar(&iterations, "iterations", 100, "Number of requests per goroutine")
	flag.IntVar(&conc, "c", 5, "Number of concurrent goroutines")
	flag.StringVar(&pprofDir, "pprof-dir", "./pprof_out", "Directory for pprof files")
	flag.Parse()

	os.MkdirAll(pprofDir, 0755)

	cpuPath := filepath.Join(pprofDir, "scupage_list_cpu.prof")
	memPath := filepath.Join(pprofDir, "scupage_list_mem.prof")
	mutexPath := filepath.Join(pprofDir, "scupage_list_mutex.prof")
	blockPath := filepath.Join(pprofDir, "scupage_list_block.prof")

	// URL для профилирования — вызывает ListWithTurbo
	testURL := baseURL + "/shop/dom-i-remont?sort=price_desc&page=1&limit=60"

	fmt.Println("=== SCUPage ListWithTurbo Profiling ===")
	fmt.Printf("URL:        %s\n", baseURL)
	fmt.Printf("Test URL:   %s\n", testURL)
	fmt.Printf("Iterations: %d per goroutine (total=%d)\n", iterations, iterations*conc)
	fmt.Printf("Conc:       %d\n", conc)
	fmt.Printf("Profiles:   %s\n", pprofDir)
	fmt.Println()

	// Warmup
	fmt.Println("Warmup...")
	_, _ = doRequest(testURL)
	time.Sleep(200 * time.Millisecond)

	// Запускаем запросы в фоне
	var wg sync.WaitGroup
	startRequests := make(chan struct{})
	totalReqs := iterations * conc
	done := make(chan struct{})

	fmt.Printf("Starting %d goroutines, %d total requests...\n", conc, totalReqs)
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startRequests
			for j := 0; j < iterations; j++ {
				_, _ = doRequest(testURL)
			}
		}(i)
	}

	// Ждём 1 сек чтобы все goroutine запустились
	time.Sleep(1 * time.Second)
	close(startRequests)

	// Запускаем CPU профилирование на 10 секунд
	fmt.Println("CPU profiling for 10s...")
	cpuProfileURL := baseURL + "/debug/pprof/profile?seconds=10"
	resp, err := http.Get(cpuProfileURL)
	if err != nil {
		fmt.Printf("Error getting CPU profile: %v\n", err)
	} else {
		cpuFile, _ := os.Create(cpuPath)
		_, _ = io.Copy(cpuFile, resp.Body)
		resp.Body.Close()
		cpuFile.Close()
		fmt.Printf("CPU profile saved: %s\n", cpuPath)
	}

	// Ждём завершения всех запросов
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("All requests completed")
	case <-time.After(15 * time.Second):
		fmt.Println("Timeout waiting for requests")
	}

	time.Sleep(500 * time.Millisecond)

	// Memory profile
	fmt.Println("Memory profile...")
	memProfileURL := baseURL + "/debug/pprof/heap"
	resp, err = http.Get(memProfileURL)
	if err != nil {
		fmt.Printf("Error getting memory profile: %v\n", err)
	} else {
		memFile, _ := os.Create(memPath)
		_, _ = io.Copy(memFile, resp.Body)
		resp.Body.Close()
		memFile.Close()
		fmt.Printf("Memory profile saved: %s\n", memPath)
	}

	// Mutex profile
	fmt.Println("Mutex profile...")
	mutexProfileURL := baseURL + "/debug/pprof/mutex"
	resp, err = http.Get(mutexProfileURL)
	if err != nil {
		fmt.Printf("Error getting mutex profile: %v\n", err)
	} else {
		mutexFile, _ := os.Create(mutexPath)
		_, _ = io.Copy(mutexFile, resp.Body)
		resp.Body.Close()
		mutexFile.Close()
		fmt.Printf("Mutex profile saved: %s\n", mutexPath)
	}

	// Block profile
	fmt.Println("Block profile...")
	blockProfileURL := baseURL + "/debug/pprof/block"
	resp, err = http.Get(blockProfileURL)
	if err != nil {
		fmt.Printf("Error getting block profile: %v\n", err)
	} else {
		blockFile, _ := os.Create(blockPath)
		_, _ = io.Copy(blockFile, resp.Body)
		resp.Body.Close()
		blockFile.Close()
		fmt.Printf("Block profile saved: %s\n", blockPath)
	}

	fmt.Println()
	fmt.Println("=== Done ===")
	fmt.Printf("View CPU profile:   go tool pprof -http=:8080 %s\n", cpuPath)
	fmt.Printf("View memory profile: go tool pprof -http=:8081 %s\n", memPath)
}

func doRequest(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
