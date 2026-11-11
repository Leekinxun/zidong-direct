import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig(({ mode }) => {
  const desktop = mode === 'desktop';
  const targets = desktop ? ['chrome108'] : ['chrome109', 'firefox115'];

  return {
    plugins: [react()],
    build: {
      target: targets,
      cssTarget: targets,
      outDir: desktop ? 'dist-desktop' : 'dist',
      sourcemap: false,
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: 'http://127.0.0.1:8080',
          changeOrigin: true,
        },
      },
    },
  };
});
