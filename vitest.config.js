import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.js'

// 复用 vite.config.js 的插件与 @ 别名，只追加测试相关配置
export default mergeConfig(viteConfig, defineConfig({
  test: {
    environment: 'jsdom',
    include: ['tests/**/*.spec.js'],
    setupFiles: ['tests/setup.js'],
    // 每个用例后自动还原 vi.spyOn / vi.stubGlobal，避免用例间串扰
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: 'v8',
      include: ['src/utils/**', 'src/composables/**', 'src/router/**'],
      reporter: ['text', 'text-summary', 'json-summary', 'html'],
      // 覆盖率门槛只升不降（见 docs/testing-ci-implementation-plan.md 4.3）：
      // - 全局：统计范围内的实测基线向下取整，P1 用例补齐后提到 80%
      // - P0 文件：已有完整用例，单独设高门槛防止回退
      //   （router 的 functions 不设门槛：懒加载路由的 () => import() 在单测中不会执行）
      thresholds: {
        lines: 55,
        statements: 55,
        branches: 90,
        functions: 66,
        '{src/composables/{useInteractionToggle,useBreakpoint,useAppShell,useUnreadNotice,useNoticeStream,useCommentSubmit,usePostReview,usePostShare}.js,src/utils/{request,sanitize,throttle,guest-access,mention,mentionResolve,mobileNav,share,agentAutoReply,apiError,follow,message}.js,src/router/**}': {
          lines: 95,
          statements: 95,
          branches: 90
        }
      }
    }
  }
}))
