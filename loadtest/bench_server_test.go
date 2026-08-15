package loadtest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	serverBaseURL = "http://localhost:9090"
	pprofOutDir   = "/home/ihar/IdeaProjects/makoshop/loadtest/pprof_out"
)

func TestMain(m *testing.M) {
	resp, err := http.Get(serverBaseURL + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server not reachable: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()

	if err := os.MkdirAll(pprofOutDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "MkdirAll: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

// BenchmarkProductSearchTurbo — benchmark /products/turbo?q=... endpoint
func BenchmarkProductSearchTurbo(b *testing.B) {
	client := &http.Client{Timeout: 5 * time.Second}

	// Start CPU profile on server (5s)
	cpuFile, err := os.Create(pprofOutDir + "/server_cpu.prof")
	if err != nil {
		b.Fatalf("create cpu prof: %v", err)
	}
	defer cpuFile.Close()

	// Start memory profile on server
	memFile, err := os.Create(pprofOutDir + "/server_mem.prof")
	if err != nil {
		b.Fatalf("create mem prof: %v", err)
	}
	defer memFile.Close()

	// Start block profile on server
	blockFile, err := os.Create(pprofOutDir + "/server_block.prof")
	if err != nil {
		b.Fatalf("create block prof: %v", err)
	}
	defer blockFile.Close()

	// Start mutex profile on server
	mutexFile, err := os.Create(pprofOutDir + "/server_mutex.prof")
	if err != nil {
		b.Fatalf("create mutex prof: %v", err)
	}
	defer mutexFile.Close()

	// Trigger server-side profiles in background
	go func() {
		// Memory
		resp, _ := client.Get(serverBaseURL + "/debug/pprof/heap")
		if resp != nil {
			io.Copy(memFile, resp.Body)
			resp.Body.Close()
		}
	}()

	go func() {
		// Block
		resp, _ := client.Get(serverBaseURL + "/debug/pprof/block")
		if resp != nil {
			io.Copy(blockFile, resp.Body)
			resp.Body.Close()
		}
	}()

	go func() {
		// Mutex
		resp, _ := client.Get(serverBaseURL + "/debug/pprof/mutex")
		if resp != nil {
			io.Copy(mutexFile, resp.Body)
			resp.Body.Close()
		}
	}()

	// Start CPU profiling on server (5s)
	cpuDone := make(chan struct{})
	go func() {
		resp, _ := client.Get(serverBaseURL + "/debug/pprof/profile?seconds=5")
		if resp != nil {
			io.Copy(cpuFile, resp.Body)
			resp.Body.Close()
		}
		close(cpuDone)
	}()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", serverBaseURL+"/products/turbo?q=tire&limit=10", nil)
			resp, err := client.Do(req)
			if err != nil {
				b.Errorf("request failed: %v", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
	b.StopTimer()

	// Wait for CPU profile to finish
	<-cpuDone
}

// BenchmarkCategoryProducts — benchmark /products?category_id=X endpoint
func BenchmarkCategoryProducts(b *testing.B) {
	client := &http.Client{Timeout: 5 * time.Second}

	cpuFile, _ := os.Create(pprofOutDir + "/server_cpu_cat.prof")
	defer cpuFile.Close()

	memFile, _ := os.Create(pprofOutDir + "/server_mem_cat.prof")
	defer memFile.Close()

	go func() {
		resp, _ := client.Get(serverBaseURL + "/debug/pprof/heap")
		if resp != nil {
			io.Copy(memFile, resp.Body)
			resp.Body.Close()
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", serverBaseURL+"/products?category_id=9&limit=20", nil)
			resp, err := client.Do(req)
			if err != nil {
				b.Errorf("request failed: %v", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

// BenchmarkHealth — baseline: just health check (network + HTTP overhead)
func BenchmarkHealth(b *testing.B) {
	client := &http.Client{Timeout: 5 * time.Second}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(serverBaseURL + "/health")
			if err != nil {
				b.Errorf("request failed: %v", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}
