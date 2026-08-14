import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

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
      },
      // Proxy /shop/* to backend for SSR (HTML with data)
      '/shop': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        followRedirects: false,
      },
      // Proxy /categories/* to backend for public category data
      '/categories': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        followRedirects: false,
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
