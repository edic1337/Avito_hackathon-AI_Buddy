import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5174,
    strictPort: true,
    proxy: {
      '/api/question': {
        // The local Docker frontend proxies the request with the intern role.
        target: 'http://127.0.0.1:5173',
      },
    },
  },
})
