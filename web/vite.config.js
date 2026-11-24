import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { resolve } from 'path'
import { copyFileSync } from 'fs'

// /landing 与 /landing/ 重定向到 public/landing/index.html（独立静态宣传页，避免被 SPA 回退接管）
function landingRedirect(req, res, next) {
  const path = (req.url || '').split('?')[0]
  if (path === '/landing' || path === '/landing/') {
    res.statusCode = 302
    res.setHeader('Location', '/landing/index.html')
    res.end()
    return
  }
  next()
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    {
      name: 'landing-redirect',
      configureServer(server) {
        server.middlewares.use(landingRedirect)
      },
      configurePreviewServer(server) {
        server.middlewares.use(landingRedirect)
      }
    },
    // 构建完成后自动创建 404.html（GitHub Pages SPA 支持）
    // apply: 'build'：dev / vitest 的服务器关闭时同样会触发 closeBundle，此时没有 dist/，需排除
    {
      name: 'generate-404',
      apply: 'build',
      closeBundle() {
        try {
          copyFileSync(
            resolve(__dirname, 'dist/index.html'),
            resolve(__dirname, 'dist/404.html')
          )
          console.log('✓ 404.html created for GitHub Pages SPA support')
        } catch (err) {
          console.error('Failed to create 404.html:', err)
        }
      }
    }
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    port: 5173,
    open: true
  },
  base: '/',
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor': ['vue', 'vue-router'],
          'ui': ['naive-ui']
        }
      }
    }
  }
})
