#!/usr/bin/env bash
#
# deploy.sh — build and deploy Makoshop to production (wszyst.pl).
#
# Usage:
#   ./deploy.sh
#   GOMEMLIMIT=8GiB ./deploy.sh    # override the soft memory limit
#
# Requires: rsync, ssh access to root@wszyst.pl
#

set -euo pipefail

cd "$(dirname "$0")"

# Soft memory limit for the Go runtime, applied via a systemd drop-in
# (makoshop.service.d/memlimit.conf, idempotent). Near the limit the GC runs
# harder instead of letting RSS grow until the kernel OOM killer fires during
# large price-file imports. Note: mmap'd DB pages are file-backed and do NOT
# count towards this limit, only the Go heap does.
GOMEMLIMIT="${GOMEMLIMIT:-6GiB}"

echo "=== Building ==="
./build.sh

echo "=== Deploying to root@wszyst.pl:/opt/makoshop/ ==="
rsync -avz --delete \
  --exclude '.git' \
  --exclude 'node_modules' \
  --exclude '_tmp' \
  --exclude 'data' \
  --exclude 'prices' \
  --exclude '.gocache' \
  makoshop frontend/dist/ root@wszyst.pl:/opt/makoshop/

echo "=== Ensuring GOMEMLIMIT=$GOMEMLIMIT (makoshop.service drop-in) ==="
ssh root@wszyst.pl "mkdir -p /etc/systemd/system/makoshop.service.d && printf '[Service]\nEnvironment=GOMEMLIMIT=${GOMEMLIMIT}\n' > /etc/systemd/system/makoshop.service.d/memlimit.conf && systemctl daemon-reload"

echo "=== Restarting service ==="
ssh root@wszyst.pl "systemctl restart makoshop"

echo "=== Done ==="
