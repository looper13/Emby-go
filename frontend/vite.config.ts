import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

const backend = process.env.EMBY_DEV_PROXY || 'http://127.0.0.1:18080';

export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  base: '/web/ui/',
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    manifest: true,
    sourcemap: false,
  },
  server: {
    proxy: Object.fromEntries(
      ['/api', '/Users', '/Items', '/Shows', '/Videos', '/Sessions', '/System', '/emby', '^/admin(?:$|[?#])', '^/web/(?!ui/)'].map(prefix => [prefix, {
        target: backend,
        changeOrigin: false,
        followRedirects: false,
        timeout: 0,
        proxyTimeout: 0,
      }]),
    ),
  },
  test: {
    setupFiles: ['tests/setup.ts'],
    environment: 'jsdom',
    include: ['tests/**/*.test.ts', 'tests/**/*.test.tsx'],
    clearMocks: true,
    restoreMocks: true,
  },
});
