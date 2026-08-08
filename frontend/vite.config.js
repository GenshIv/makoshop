import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      // Proxy API routes to backend, exclude vite internal paths and static assets
      '/products': { target: 'http://localhost:9090', changeOrigin: true },
      '/auth': { target: 'http://localhost:9090', changeOrigin: true },
      '/cart': { target: 'http://localhost:9090', changeOrigin: true },
      '/orders': { target: 'http://localhost:9090', changeOrigin: true },
      '/payments': { target: 'http://localhost:9090', changeOrigin: true },
      '/users': { target: 'http://localhost:9090', changeOrigin: true },
      '/reviews': { target: 'http://localhost:9090', changeOrigin: true },
      '/companies': { target: 'http://localhost:9090', changeOrigin: true },
      '/admin': { target: 'http://localhost:9090', changeOrigin: true },
      '/categories': { target: 'http://localhost:9090', changeOrigin: true },
      '/promo': { target: 'http://localhost:9090', changeOrigin: true },
    },
  },
})
