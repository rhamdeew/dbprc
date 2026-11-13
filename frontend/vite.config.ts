/// <reference types="vitest/config" />
import fs from 'node:fs';
import path from 'node:path';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

const api = process.env.API_URL ?? 'http://localhost:8080';
// `make dev` starts the API in backend/ with the default UPLOAD_DIR=./uploads.
const uploadDir = path.resolve(import.meta.dirname, process.env.UPLOAD_DIR ?? '../backend/uploads');

const uploadTypes: Record<string, string> = { '.jpg': 'image/jpeg', '.png': 'image/png' };

/**
 * `make dev` only: serve /uploads from the API's upload directory.
 * In staging and prod the web container does this, and the API never serves them
 * unless STATIC_DIR is set. Without this, vite answers an image URL with index.html.
 */
function devUploads(): Plugin {
  return {
    name: 'arc-dev-uploads',
    configureServer(server) {
      server.middlewares.use('/uploads', (req, res) => {
        let rel: string;
        try {
          rel = decodeURIComponent((req.url ?? '').split('?')[0] ?? '');
        } catch {
          rel = '';
        }
        const file = path.join(uploadDir, rel);
        const inside = file.startsWith(uploadDir + path.sep);
        if (!inside || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
          // A real 404, not the SPA fallback: the browser must not get HTML for an image.
          res.statusCode = 404;
          res.end();
          return;
        }
        res.setHeader('Content-Type', uploadTypes[path.extname(file)] ?? 'application/octet-stream');
        res.setHeader('X-Content-Type-Options', 'nosniff');
        res.setHeader('Content-Disposition', 'inline');
        fs.createReadStream(file).pipe(res);
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), devUploads()],
  server: {
    port: 5173,
    proxy: {
      '/api': api,
      '/auth': api,
      '/healthz': api,
      '/ws': { target: api.replace(/^http/, 'ws'), ws: true },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/main.tsx', 'src/test/**', '**/*.test.{ts,tsx}'],
    },
  },
});
