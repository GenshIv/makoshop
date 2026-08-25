import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

const BACKEND = 'http://localhost:9090'

// Helper to forward custom header X-Response-Time-Ms from backend to browser
const forwardResponseTimeHeader = (proxyReq, req, res) => {
  const originalWriteHead = res.writeHead;
  res.writeHead = function (statusCode, statusMessage, headers) {
    if (headers && headers['X-Response-Time-Ms']) {
      // Vite may drop custom headers; we re-add via setHeader
      this.setHeader('X-Response-Time-Ms', headers['X-Response-Time-Ms']);
    }
    return originalWriteHead.apply(this, arguments);
  };
};

// Decide whether a request is an API call (proxy to backend) or a page
// navigation / static asset (serve via Vite).
//
// In production the Go server serves both the SPA and the API on the same
// origin and disambiguates via the Accept header:
//   - page navigations send Accept: text/html  -> served as index.html (SPA)
//   - API calls (axios) send Accept: application/json, text/plain, */*
//     (no literal "text/html") -> handled by the API routes
// We replicate that logic here so dev mode matches production.
function isApiRequest(req) {
  const url = req.url || '';
  const accept = req.headers['accept'] || '';

  // Vite internals and static assets are always served by Vite.
  if (
    url.startsWith('/@') ||
    url.startsWith('/src/') ||
    url.startsWith('/node_modules/') ||
    url.includes('.')
  ) {
    return false;
  }
  // Page navigations (browser requesting HTML) are served by Vite as the SPA.
  if (accept.includes('text/html')) {
    return false;
  }
  // Everything else is an API call -> proxy to backend.
  return true;
}

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  build: {
    // apexcharts (AdminStatsView) is a large third-party charting library that
    // is code-split into its own lazy chunk. Raise the warning threshold so the
    // build reports cleanly; the initial bundle stays well under 500 kB.
    chunkSizeWarningLimit: 1000,
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      // Catch-all proxy: API calls go to the backend, everything else to Vite.
      '/': {
        target: BACKEND,
        changeOrigin: true,
        followRedirects: false,
        bypass(req) {
          if (!isApiRequest(req)) {
            // Serve via Vite (static asset or SPA fallback).
            return req.url;
          }
          // Proxy to backend.
          return null;
        },
        configure: (proxy) => {
          proxy.on('proxyReq', forwardResponseTimeHeader);
        },
      },
    },
  },
})
