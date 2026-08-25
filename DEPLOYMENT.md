# Makoshop — Production Deployment Guide

This guide covers building and deploying Makoshop to a production server.

The project is a single Go binary that serves both the API and the built
frontend (SPA). No separate web server is required, though you may put one in
front for additional needs.

---

## 1. Architecture at a glance

- **Backend**: Go binary (`makoshop`) — API + serves `frontend/dist/`.
- **Storage**: `makodb` (sharded key-value store) in a data directory.
- **Frontend**: Vue 3 SPA, built to `frontend/dist/`, served by the binary.
- **Dependencies**: vendored in `vendor/` — the build is self-contained and does
  **not** require the local `makodb`/`silentjson` checkouts.

---

## 2. Build

Run on any machine with Go (>= 1.26) and Node (>= 18):

```bash
./build.sh
```

This produces:
- `makoshop` — the server binary (statically linked, stripped).
- `frontend/dist/` — the built frontend.

> The build uses `-mod=vendor`, so it works without the local module replaces.
> You do **not** need `makodb` or `silentjson` checked out to build.

---

## 3. What to deploy

Copy to the production server:

1. The `makoshop` binary.
2. The `frontend/dist/` directory (place it next to the binary, or where the
   binary expects it — see below).
3. A `.env` file (see next section).
4. Your TLS certificate and key (if using HTTPS).
5. The `vendor/` directory is **not** needed at runtime — only at build time.

The binary looks for the frontend at `frontend/dist/index.html` relative to the
working directory. So keep the same layout:

```
/opt/makoshop/
├── makoshop              # the binary
├── .env                  # configuration (auto-loaded)
└── frontend/
    └── dist/             # built frontend
```

---

## 4. Configuration (.env)

The server automatically loads `.env` from the working directory (or the binary
directory) on startup. Copy the example and fill it in:

```bash
cp .env.production.example .env
# edit .env
```

Key variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `MAKOSHOP_JWT_SECRET` | **yes** | JWT signing secret. `openssl rand -hex 32` |
| `MAKOSHOP_HOST` | no | Bind address (default `0.0.0.0`) |
| `MAKOSHOP_PORT` | no | HTTPS port (default `9090`) |
| `MAKOSHOP_TLS_CERT` | for HTTPS | Path to TLS certificate |
| `MAKOSHOP_TLS_KEY` | for HTTPS | Path to TLS key |
| `MAKOSHOP_HTTP_PORT` | no | Plain-HTTP port that redirects to HTTPS |
| `MAKOSHOP_DB_PATH` | no | makodb data dir (default `makoshop_db`) |
| `I18N_LANG` | no | Default language (default `ru`) |

> The server **refuses to start** if `MAKOSHOP_JWT_SECRET` is not set.

### TLS

To serve HTTPS directly, set both `MAKOSHOP_TLS_CERT` and `MAKOSHOP_TLS_KEY`.
Optionally set `MAKOSHOP_HTTP_PORT` to run a plain-HTTP listener that 301-
redirects all traffic to HTTPS.

For a production domain, use a real certificate (e.g. Let's Encrypt via
certbot).

---

## 5. Run

```bash
cd /opt/makoshop
./makoshop
```

You should see:
```
Makoshop API server starting on https://0.0.0.0:9090 (TLS)
```

### As a systemd service (recommended)

Create `/etc/systemd/system/makoshop.service`:

```ini
[Unit]
Description=Makoshop API server
After=network.target

[Service]
Type=simple
User=makoshop
WorkingDirectory=/opt/makoshop
ExecStart=/opt/makoshop/makoshop
Restart=always
RestartSec=5
# Environment file is auto-loaded, but you can also set it here:
# EnvironmentFile=/opt/makoshop/.env

[Install]
WantedBy=multi-user.target
```

Then:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now makoshop
sudo systemctl status makoshop
```

---

## 6. Database

- makodb stores data in `MAKOSHOP_DB_PATH` as 16 sharded `.db` files.
- **Back up this directory** — it is your entire database.
- The server holds a lock file (`.makodb.lock`); only one instance can run per
  data directory.

---

## 7. Health check

```bash
curl -k https://localhost:9090/health   # -> "ok"
```

---

## 8. Notes

- **Payments** are currently disabled (all payment endpoints return 503).
- **Security headers** (CSP, X-Frame-Options, etc.) and request timeouts are
  applied automatically.
- The frontend and API share the same origin; the server disambiguates page
  navigations (HTML) from API calls (JSON) via the `Accept` header.
