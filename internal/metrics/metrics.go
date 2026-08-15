package metrics

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry — одна запись метрики запроса.
type Entry struct {
	TimestampMs int64  `json:"ts"`
	TimeStart   int64  `json:"time_start"`
	DurationNs  int64  `json:"time_run"`
	URL         string `json:"url"`
	Method      string `json:"method"`
	Referer     string `json:"referer"`
	IP          string `json:"ip"`
	UserAgent   string `json:"user_agent"`
	Code        int    `json:"code_ans"`
}

// Writer — фоновый writer метрик в JSONL-файлы.
type Writer struct {
	ch       chan Entry
	wg       sync.WaitGroup
	mu       sync.Mutex
	file     *os.File
	fileSize int64
	maxSize  int64 // макс размер одного файла в байтах
	dir      string
	bufSize  int
	interval time.Duration
}

// NewWriter создаёт writer, который пишет в dir/metrics-YYYY-MM-DD.jsonl батчами.
// maxSize — максимальный размер одного файла в байтах (например 50*1024*1024 = 50 МБ).
func NewWriter(dir string, bufSize int, interval time.Duration, maxSize int64) (*Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	name := fmt.Sprintf("metrics-%s-%d.jsonl", time.Now().Format("2006-01-02"), time.Now().Unix())
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	info, _ := f.Stat()
	fileSize := int64(0)
	if info != nil {
		fileSize = info.Size()
	}

	mw := &Writer{
		ch:       make(chan Entry, 10000),
		file:     f,
		fileSize: fileSize,
		maxSize:  maxSize,
		dir:      dir,
		bufSize:  bufSize,
		interval: interval,
	}

	mw.wg.Add(1)
	go mw.run()

	return mw, nil
}

func (mw *Writer) run() {
	defer mw.wg.Done()
	defer mw.file.Close()

	buf := make([]Entry, 0, mw.bufSize)
	ticker := time.NewTicker(mw.interval)
	defer ticker.Stop()

	for {
		select {
		case e, ok := <-mw.ch:
			if !ok {
				mw.flush(buf)
				return
			}
			buf = append(buf, e)
			if len(buf) >= mw.bufSize {
				mw.flush(buf)
			}
		case <-ticker.C:
			if len(buf) > 0 {
				mw.flush(buf)
			}
		}
	}
}

func (mw *Writer) flush(buf []Entry) {
	if len(buf) == 0 {
		return
	}

	// Timestamp берём раз на батч в фоновой горутине, не на пути запроса.
	ts := time.Now().UnixMilli()
	for i := range buf {
		buf[i].TimestampMs = ts
	}

	var b []byte
	for i := range buf {
		if i > 0 {
			b = append(b, '\n')
		}
		j, _ := json.Marshal(&buf[i])
		b = append(b, j...)
	}
	b = append(b, '\n')

	mw.mu.Lock()
	defer mw.mu.Unlock()

	// Если файл слишком большой — ротация
	if mw.fileSize+int64(len(b)) > mw.maxSize {
		_ = mw.file.Close()
		name := fmt.Sprintf("metrics-%s-%d.jsonl", time.Now().Format("2006-01-02"), time.Now().Unix())
		path := filepath.Join(mw.dir, name)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			// если не можем создать новый — бросаем метрики
			return
		}
		mw.file = f
		mw.fileSize = 0
	}

	n, _ := mw.file.Write(b)
	mw.fileSize += int64(n)
	buf = buf[:0]
}

// Record отправляет запись в канал. Если канал переполнен — пропускает, не тормозит запрос.
func (mw *Writer) Record(e Entry) {
	select {
	case mw.ch <- e:
	default:
		// drop if full
	}
}

// Close закрывает канал и ждёт завершения фоновой горутини (flush остатков).
func (mw *Writer) Close() {
	close(mw.ch)
	mw.wg.Wait()
}

// Middleware возвращает http middleware, собирающий метрики запросов.
func Middleware(mw *Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK, start: start}
			next.ServeHTTP(sw, r)

			durationNs := time.Since(start).Nanoseconds()

			e := Entry{
				TimestampMs: 0, // будет установлен в Writer.run
				TimeStart:   start.UnixNano(),
				DurationNs:  durationNs,
				URL:         r.URL.String(),
				Method:      r.Method,
				Referer:     r.Referer(),
				IP:          clientIP(r),
				UserAgent:   r.UserAgent(),
				Code:        sw.status,
			}

			mw.Record(e)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	start  time.Time
	wrote  bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if sw.wrote {
		return
	}
	sw.wrote = true
	sw.status = code

	// Заголовок для фронтенда: время выполнения запроса в миллисекундах.
	// Пишем ДО WriteHeader, чтобы заголовок ушёл в ответ.
	durationNs := time.Since(sw.start).Nanoseconds()
	sw.ResponseWriter.Header().Set("X-Response-Time-Ms", fmt.Sprintf("%.3f", float64(durationNs)/1e6))

	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(p []byte) (int, error) {
	if !sw.wrote {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriter.Write(p)
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
