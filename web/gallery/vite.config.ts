import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The harness builds from this folder so nothing here can reach the product
// bundle. Output goes to a scratch directory, never to dist/.
export default defineConfig({
    root: __dirname,
    plugins: [react()],
    build: { outDir: process.env.GALLERY_OUT || '../.gallery-dist', emptyOutDir: true },
});
