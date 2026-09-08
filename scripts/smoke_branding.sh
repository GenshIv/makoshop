#!/bin/bash
# Smoke test for the branding system API (server on :9091, temp DB copy).
set -u
B=http://127.0.0.1:9091
PASS=0; FAIL=0

check() { # name, expected, actual
  if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "ok   - $1";
  else FAIL=$((FAIL+1)); echo "FAIL - $1 (expected [$2], got [$3])"; fi
}

# 1. Public: active branding (no sets/overrides; version is a persistent
#    monotonic counter, so only check it is a non-negative integer)
R=$(curl -s $B/branding/active)
check "GET /branding/active (no sets/overrides)" "0|0" \
  "$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("sets") or []), end=""); print("|", end=""); print(len(d.get("category_overrides") or []))' 2>/dev/null | tr -d ' ')"
V=$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); v=d.get("version"); print(v if isinstance(v,int) and v>=0 else "bad")' 2>/dev/null)
check "GET /branding/active (version is int>=0)" "ok" "$([ "$V" != "bad" ] && echo ok || echo bad)"

# 2. Admin auth: login, register if the user does not exist yet
R=$(curl -s -X POST $B/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"smoke_branding@test.local","password":"test123456"}')
TOKEN=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
if [ -z "$TOKEN" ]; then
  R=$(curl -s -X POST $B/auth/register -H 'Content-Type: application/json' \
    -d '{"email":"smoke_branding@test.local","password":"test123456","role":"admin"}')
  TOKEN=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
fi
[ -n "$TOKEN" ] && { PASS=$((PASS+1)); echo "ok   - admin auth"; } || { FAIL=$((FAIL+1)); echo "FAIL - admin auth: $R"; }
AUTH="Authorization: Bearer $TOKEN"

# 3. Create set (disabled by default)
R=$(curl -s -X POST $B/admin/branding/sets -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "name": "Smoke Test Set",
  "description": "smoke",
  "priority": 5,
  "enabled": false,
  "elements": [
    {"slot":"header_fullwidth","image_url":"/uploads/branding/a.png","page_patterns":["^/shop/"]},
    {"slot":"home_banner","image_url":"/uploads/branding/b.png"}
  ]
}')
SET_ID=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
[ -n "$SET_ID" ] && { PASS=$((PASS+1)); echo "ok   - create set (id=$SET_ID)"; } || { FAIL=$((FAIL+1)); echo "FAIL - create set: $R"; }

# 4. Active: disabled set must NOT appear
R=$(curl -s $B/branding/active)
N=$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("sets") or []))' 2>/dev/null)
check "disabled set hidden from /branding/active" "0" "$N"

# 5. Enable set via PATCH
R=$(curl -s -X PATCH $B/admin/branding/sets/$SET_ID -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "name": "Smoke Test Set","description":"smoke","priority":5,"enabled":true,
  "elements":[{"slot":"header_fullwidth","image_url":"/uploads/branding/a.png","page_patterns":["^/shop/"]},{"slot":"home_banner","image_url":"/uploads/branding/b.png"}]
}')
EN=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("enabled"))' 2>/dev/null)
check "PATCH enables set" "True" "$EN"

# 6. Active: enabled set now appears, version bumped
R=$(curl -s $B/branding/active)
N=$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("sets") or []))' 2>/dev/null)
V=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version",0))' 2>/dev/null)
check "enabled set visible in /branding/active" "1" "$N"
[ "$V" -ge 1 ] && { PASS=$((PASS+1)); echo "ok   - version bumped ($V)"; } || { FAIL=$((FAIL+1)); echo "FAIL - version bump (got $V)"; }

# 7. Category override: pick a real category id
CAT_ID=$(curl -s $B/categories | python3 -c 'import json,sys; d=json.load(sys.stdin); items=d.get("items") or []; print(items[0]["id"] if items else "")' 2>/dev/null)
R=$(curl -s -X POST $B/admin/branding/category-overrides -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"category_id\":$CAT_ID,\"slot\":\"category_banner\",\"image_url\":\"/uploads/branding/c.png\"}")
check "create category override" "category_banner" \
  "$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("slot",""))' 2>/dev/null)"

# 8. Override visible in active payload
R=$(curl -s $B/branding/active)
N=$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("category_overrides") or []))' 2>/dev/null)
check "override visible in /branding/active" "1" "$N"

# 9. Delete override
R=$(curl -s -X DELETE "$B/admin/branding/category-overrides?category_id=$CAT_ID&slot=category_banner" -H "$AUTH")
check "delete override" "deleted" "$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null)"

# 10. Invalid slot rejected
R=$(curl -s -o /dev/null -w "%{http_code}" -X POST $B/admin/branding/sets -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"Bad","elements":[{"slot":"nope","image_url":"/x.png"}]}')
check "invalid slot rejected (400)" "400" "$R"

# 11. Duplicate slot in one set rejected
R=$(curl -s -o /dev/null -w "%{http_code}" -X POST $B/admin/branding/sets -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"Dup","elements":[{"slot":"home_banner","image_url":"/a.png"},{"slot":"home_banner","image_url":"/b.png"}]}')
check "duplicate slot rejected (400)" "400" "$R"

# 12. Upload with subdir=branding & max_dim
# Generate a NOISY 3000x400 PNG (random pixels don't compress well, so the
# 1920px resize genuinely shrinks the file and processCategoryImage replaces it).
cat > _tmp/noisy_gen.go << 'GOEOF'
package main

import (
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
)

func main() {
	w, h := 3000, 400
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(42))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)),
				B: uint8(rng.Intn(256)), A: 255,
			})
		}
	}
	f, _ := os.Create("_tmp/smoke_test.png")
	defer f.Close()
	png.Encode(f, img)
}
GOEOF
GOCACHE=$PWD/.gocache go run _tmp/noisy_gen.go
R=$(curl -s -X POST $B/admin/upload-image -H "$AUTH" \
  -F "file=@_tmp/smoke_test.png" -F "subdir=branding" -F "max_dim=1920")
U=$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("url",""))' 2>/dev/null)
case "$U" in /uploads/branding/*) PASS=$((PASS+1)); echo "ok   - upload branding subdir ($U)";; *) FAIL=$((FAIL+1)); echo "FAIL - upload: $R";; esac
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$B$U")
check "uploaded file served" "200" "$CODE"
# Verify it was resized (3000px -> 1920px max side)
python3 -c "
import struct, urllib.request
data = urllib.request.urlopen('$B$U').read()
w, h = struct.unpack('>II', data[16:24])
assert max(w, h) <= 1920, f'not resized: {w}x{h}'
print(f'resized to {w}x{h}')
" && { PASS=$((PASS+1)); echo "ok   - image resized to max_dim"; } || { FAIL=$((FAIL+1)); echo "FAIL - resize check"; }

# 13. Delete set
R=$(curl -s -X DELETE $B/admin/branding/sets/$SET_ID -H "$AUTH")
check "delete set" "deleted" "$(echo "$R" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null)"

# 14. Active: back to empty
R=$(curl -s $B/branding/active)
N=$(echo "$R" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("sets") or []))' 2>/dev/null)
check "active empty after delete" "0" "$N"

echo ""
echo "=== SMOKE RESULT: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
