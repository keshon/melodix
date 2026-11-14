import { defineConfig } from 'vite';
import { documentationPlugin } from './scripts/documentation.mjs';

export default defineConfig({
  base: process.env.SITE_BASE_PATH || '/',
  plugins: [documentationPlugin()],
});
