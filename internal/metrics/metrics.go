package metrics

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// Entry — заглушка для совместимости.
type Entry struct{}

// Writer — заглушка. Метрики отключены.
type Writer struct{}

// NewWriter возвращает заглушку.
func NewWriter(dir string, bufSize int, interval time.Duration, maxSize int64) (*Writer, error) {
	return &Writer{}, nil
}

// Record — no-op.
func (mw *Writer) Record(e Entry) {}

// Close — no-op.
func (mw *Writer) Close() {}

// Middleware возвращает no-op middleware (оставлен для совместимости).
func Middleware(mw *Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)

			// X-Response-Time-Ms оставляем для фронтенда.
			durationNs := time.Since(start).Nanoseconds()
			w.Header().Set("X-Response-Time-Ms", fmt.Sprintf("%.3f", float64(durationNs)/1e6))
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := findByte(xff, ','); idx >= 0 {
			return xff[:idx]
		}
		return xff
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	if r.RemoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func findByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
