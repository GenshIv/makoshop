#!/usr/bin/env bash
set -euo pipefail

# profile_concurrent.sh — запускает сервер, гоняет нагрузку и собирает профили.
#
# Использование:
#   ./scripts/profile_concurrent.sh [BASE_URL] [CONCURRENCY] [DURATION]
#
# Пример:
#   ./scripts/profile_concurrent.sh http://localhost:9090 100 30s

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

BASE_URL="${1:-http://localhost:9090}"
CONCURRENCY="${2:-100}"
DURATION="${3:-30s}"
PPROF_DIR="./pprof_out"

mkdir -p "$PPROF_DIR"

echo "=== MakoShop Concurrent Profiling ==="
echo "BASE_URL:    $BASE_URL"
echo "Concurrency: $CONCURRENCY"
echo "Duration:    $DURATION"
echo "Profiles:    $PPROF_DIR"
echo

# Проверяем, жив ли сервер
echo "Checking server health..."
for i in $(seq 1 10); do
    if curl -sf "$BASE_URL/health" >/dev/null 2>&1; then
        echo "Server is up."
        break
    fi
    if [ "$i" -eq 10 ]; then
        echo "ERROR: server not responding at $BASE_URL/health after 10 attempts."
        exit 1
    fi
    sleep 1
done

echo "Warming up..."
curl -sf "$BASE_URL/shop" >/dev/null
curl -sf "$BASE_URL/categories/tree" >/dev/null
curl -sf "$BASE_URL/products/turbo?q=test&limit=20" >/dev/null
sleep 1

echo "Starting load test: -c $CONCURRENCY -d $DURATION ..."
cd "$PROJECT_DIR/loadtest/cmd/load"
go run . \
    -url "$BASE_URL" \
    -c "$CONCURRENCY" \
    -d "$DURATION" &
LOAD_PID=$!
cd "$PROJECT_DIR"

# Даем нагрузке стартовать
sleep 3

echo "Collecting CPU profile (10s)..."
CPU_PROF="$PPROF_DIR/cpu_concurrent.prof"
curl -s "$BASE_URL/debug/pprof/profile?seconds=10" -o "$CPU_PROF"
echo "  -> $CPU_PROF ($(wc -c < "$CPU_PROF") bytes)"

echo "Collecting heap profile..."
HEAP_PROF="$PPROF_DIR/heap_concurrent.prof"
curl -s "$BASE_URL/debug/pprof/heap" -o "$HEAP_PROF"
echo "  -> $HEAP_PROF ($(wc -c < "$HEAP_PROF") bytes)"

echo "Collecting mutex profile..."
MUTEX_PROF="$PPROF_DIR/mutex_concurrent.prof"
curl -s "$BASE_URL/debug/pprof/mutex" -o "$MUTEX_PROF"
echo "  -> $MUTEX_PROF ($(wc -c < "$MUTEX_PROF") bytes)"

echo "Collecting block profile..."
BLOCK_PROF="$PPROF_DIR/block_concurrent.prof"
curl -s "$BASE_URL/debug/pprof/block" -o "$BLOCK_PROF"
echo "  -> $BLOCK_PROF ($(wc -c < "$BLOCK_PROF") bytes)"

echo "Collecting goroutine profile..."
GOR_PROF="$PPROF_DIR/goroutine_concurrent.prof"
curl -s "$BASE_URL/debug/pprof/goroutine?debug=1" -o "$GOR_PROF"
echo "  -> $GOR_PROF ($(wc -c < "$GOR_PROF") bytes)"

# Ждём завершения нагрузки
wait $LOAD_PID || true

echo
echo "=== Done ==="
echo
echo "View profiles:"
echo "  CPU:      go tool pprof -http=:8080 $CPU_PROF"
echo "  Heap:     go tool pprof -http=:8081 $HEAP_PROF"
echo "  Mutex:    go tool pprof -http=:8082 $MUTEX_PROF"
echo "  Block:    go tool pprof -http=:8083 $BLOCK_PROF"
echo "  Goroutine: less $GOR_PROF"
echo
echo "Or use 'go tool pprof <file>' interactively."
