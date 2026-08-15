import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

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

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      // Proxy /api/* to backend, strip /api prefix
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        followRedirects: false,
        rewrite: (path) => path.replace(/^\/api/, ''),
        configure: (proxy) => {
          proxy.on('proxyReq', forwardResponseTimeHeader);
        },
      },
      // Proxy /shop/* to backend for SSR (HTML with data)
      '/shop': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        followRedirects: false,
        configure: (proxy) => {
          proxy.on('proxyReq', forwardResponseTimeHeader);
        },
      },
      // Proxy /categories/* to backend for public category data
      '/categories': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        followRedirects: false,
        configure: (proxy) => {
          proxy.on('proxyReq', forwardResponseTimeHeader);
        },
      },
      // Proxy robots.txt and sitemap to backend
      '/robots.txt': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/sitemap.xml': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/sitemap-categories.xml': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/sitemap-scupage': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
    },
  },
})
