/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    // В `npm run dev` запросы к /api уходят на локальный бэкенд, как через nginx в Docker.
    proxy: {
      '/api': { target: process.env.API_PROXY_TARGET ?? 'http://localhost:8080', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov', 'json-summary'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/main.tsx',
        'src/router.tsx',
        'src/mocks/browser.ts',
        'src/vite-env.d.ts',
      ],
      thresholds: { lines: 80, statements: 80, functions: 80, branches: 75 },
    },
  },
});
