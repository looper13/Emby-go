import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';

const backend = process.env.EMBY_DEV_PROXY || 'http://127.0.0.1:18080';

export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  base: '/web/ui/',
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    manifest: true,
    sourcemap: false,
  },
  server: {
    proxy: Object.fromEntries(
      ['/api', '/Users', '/Items', '/Videos', '/System', '/emby', '^/admin(?:$|[?#])', '^/web/(?!ui/)'].map(prefix => [prefix, {
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
    include: ['tests/**/*.test.ts'],
    clearMocks: true,
    restoreMocks: true,
  },
});
