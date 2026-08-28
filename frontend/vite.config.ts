import { defineConfig, loadEnv, Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import checker from 'vite-plugin-checker'
import { resolve } from 'path'
import { createRequire } from 'module'

// 从 @phala/dcap-qvl 自身的位置解析它声明的 `buffer` polyfill，拿到绝对路径。
// 走依赖图而不是写死 node_modules 路径，pnpm 的严格布局和 CI 的 hoisted
// 布局下都成立，且拿到的一定是 dcap-qvl 实际使用的那份。
const requireFromHere = createRequire(resolve(__dirname, 'vite.config.ts'))
const bufferPolyfillPath = createRequire(
  requireFromHere.resolve('@phala/dcap-qvl/package.json')
).resolve('buffer/')

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character] || character)
}

function isSafeImageUrl(value: string): boolean {
  const trimmed = value.trim()
  if ((trimmed.startsWith('/') && !trimmed.startsWith('//')) || /^data:image\//i.test(trimmed)) {
    return true
  }
  try {
    const parsed = new URL(trimmed)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function injectBranding(html: string, config: { site_name?: string; site_logo?: string }): string {
  let brandedHtml = html
  const siteName = config.site_name?.trim()
  if (siteName) {
    brandedHtml = brandedHtml.replace(
      /<title>[^<]*<\/title>/i,
      `<title>${escapeHtml(siteName)} - AI API Gateway</title>`,
    )
  }

  const siteLogo = config.site_logo?.trim()
  if (siteLogo && isSafeImageUrl(siteLogo)) {
    brandedHtml = brandedHtml.replace(
      /<link\s+rel=["']icon["'][^>]*>/i,
      `<link rel="icon" href="${escapeHtml(siteLogo)}" />`,
    )
  }
  return brandedHtml
}

/**
 * Vite 插件：开发模式下注入公开配置到 index.html
 * 与生产模式的后端注入行为保持一致，消除闪烁
 */
function injectPublicSettings(backendUrl: string): Plugin {
  return {
    name: 'inject-public-settings',
    apply: 'serve',
    transformIndexHtml: {
      order: 'pre',
      async handler(html) {
        try {
          const response = await fetch(`${backendUrl}/api/v1/settings/public`, {
            signal: AbortSignal.timeout(2000)
          })
          if (response.ok) {
            const data = await response.json()
            if (data.code === 0 && data.data) {
              const script = `<script>window.__APP_CONFIG__=${JSON.stringify(data.data)};</script>`
              return injectBranding(html, data.data).replace('</head>', `${script}\n</head>`)
            }
          }
        } catch (e) {
          console.warn('[vite] 无法获取公开配置，将回退到 API 调用:', (e as Error).message)
        }
        return html
      }
    }
  }
}

/**
 * APEXONE-EXT: 构建时预压缩静态资源。
 *
 * 后端 internal/web/precompressed.go 会在客户端支持 gzip 时优先发这里生成的
 * .gz，找不到就原样发原文。所以这个插件的产物是**纯优化**，缺了不会坏，
 * 只是慢——实测那个 250KB 的 CSS 压到 33KB。
 *
 * 放在 vite 构建里而不是单独一个 npm script，是为了让它不可能被忘记：
 * 任何人跑 `pnpm build` 都会带上，不需要记得多跑一步。
 *
 * 用 Node 内置 zlib，不引第三方插件——这点活不值得多一个依赖。
 */
function precompressAssets(): Plugin {
  return {
    name: 'apexone-precompress',
    apply: 'build',
    // writeBundle 而不是 generateBundle：要等文件真的落盘，且此时拿到的是
    // 最终产物（含 vite 自己的后处理）。
    async writeBundle(options, bundle) {
      const { gzipSync } = await import('zlib')
      const { writeFileSync, readFileSync } = await import('fs')
      const { join, extname } = await import('path')
      const outDir = options.dir || ''
      const compressible = new Set(['.js', '.css', '.html', '.json', '.svg', '.txt', '.xml'])
      let saved = 0
      for (const name of Object.keys(bundle)) {
        if (!compressible.has(extname(name).toLowerCase())) continue
        const file = join(outDir, name)
        try {
          const raw = readFileSync(file)
          // 小文件压了也省不了多少，反而多一次文件打开。1KB 以下跳过。
          if (raw.length < 1024) continue
          const gz = gzipSync(raw, { level: 9 })
          // 压完反而更大就别写了（已压缩的内容会这样）。
          if (gz.length >= raw.length) continue
          writeFileSync(file + '.gz', gz)
          saved += raw.length - gz.length
        } catch {
          // 单个文件失败不该让整个构建失败：后端会退回发原文。
        }
      }
      if (saved > 0) {
        this.info(`预压缩完成，省下 ${(saved / 1024).toFixed(0)} KB`)
      }
    },
  }
}

export default defineConfig(({ mode }) => {
  // 加载环境变量
  const env = loadEnv(mode, process.cwd(), '')
  const backendUrl = env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080'
  const devPort = Number(env.VITE_DEV_PORT || 3000)

  return {
    plugins: [
      vue(),
      checker({
        vueTsc: true
      }),
      injectPublicSettings(backendUrl),
      precompressAssets()
    ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      // 使用 vue-i18n 运行时版本，避免 CSP unsafe-eval 问题
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js',
      // @phala/dcap-qvl 的依赖链（asn1.js → safer-buffer）里有裸 `require('buffer')`。
      // Vite 默认把它当成 Node 内置模块外部化为空对象，safer-buffer 求值时会去读
      // `undefined.prototype` 而直接抛 TypeError —— /proof 的远程证明验证会整块失效，
      // 而构建只报一条 warning，很容易漏掉。dcap-qvl 本身就把 `buffer` 这个纯 JS
      // polyfill 列为依赖，这里显式指过去。
      buffer: bufferPolyfillPath
    }
  },
  define: {
    // 启用 vue-i18n JIT 编译，在 CSP 环境下处理消息插值
    // JIT 编译器生成 AST 对象而非 JS 代码，无需 unsafe-eval
    __INTLIFY_JIT_COMPILATION__: true
  },
  build: {
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
    /**
     * 只预载入口真正需要的那几个 chunk，不递归预载懒加载路由的依赖。
     *
     * 默认策略（resolveDependencies 返回全部 deps）会把懒加载路由的依赖也写进
     * index.html 的 modulepreload，浏览器于是在首屏就把它们全下下来——懒加载
     * 因此形同虚设。实测：首屏被预载了 381KB 的 TEE 证明密码学栈，而它只有
     * /proof 那一页用；绝大多数人从不打开那一页。
     *
     * 关掉之后这些 chunk 回到「点到那一页才下」，代价是真去 /proof 的人多一次
     * 往返——那一页本来就要跑几秒的硬件验证，多这一次往返无感。
     */
    modulePreload: { polyfill: true, resolveDependencies: () => [] },
    rollupOptions: {
      output: {
        /**
         * 手动分包配置
         * 分离第三方库并按功能合并应用代码，避免循环依赖
         */
        manualChunks(id: string) {
          if (id.includes('node_modules')) {
            // Vue 核心库
            if (
              id.includes('/vue/') ||
              id.includes('/vue-router/') ||
              id.includes('/pinia/') ||
              id.includes('/@vue/')
            ) {
              return 'vendor-vue'
            }

            // UI 工具库（较大，单独分离）
            if (id.includes('/@vueuse/') || id.includes('/xlsx/')) {
              return 'vendor-ui'
            }

            // 图表库
            if (id.includes('/chart.js/') || id.includes('/vue-chartjs/')) {
              return 'vendor-chart'
            }

            // 国际化
            if (id.includes('/vue-i18n/') || id.includes('/@intlify/')) {
              return 'vendor-i18n'
            }

            // Stripe 仅在支付流程中按需加载，避免进入首页公共依赖。
            if (id.includes('/@stripe/stripe-js/')) {
              return 'vendor-stripe'
            }

            // 注：曾尝试把 TEE 证明的密码学栈（dcap-qvl / noble / elliptic /
            // asn1 家族，源码约 720KB，只有懒加载的 /proof 用得到）拆成独立
            // chunk，好让首页不必下载它。**三次尝试全部在运行时炸掉**：
            //   按包名拆、buffer 留在 vendor-misc → Cannot destructure 'Buffer'
            //   把 buffer 拉进同一个 chunk       → Cannot read 'toArray'
            //   整条依赖链一起打包               → pb is not a function
            // 这条链（asn1.js → safer-buffer → buffer，elliptic → bn.js）对模块
            // **求值顺序**敏感，而 rollup 只保证 chunk 内有序、不保证跨 chunk。
            // 每一次的现象都是整页 JS 报错（基线 headless 渲染是零错误）。
            //
            // 所以它继续留在 vendor-misc。真要拆的话，得先弄清这条链的求值依赖，
            // 而不是继续换匹配规则去试——收益是首页少下 ~400KB，不值得拿白屏赌。

            // 支付相关的第三方（二维码、airwallex 埋点）。同理：只有付款流程
            // 需要，而那几个页面同样是懒加载路由。
            if (
              id.includes('/qrcode/') ||
              id.includes('/@airwallex/') ||
              id.includes('/encode-utf8/') ||
              id.includes('/dijkstrajs/')
            ) {
              return 'vendor-payment'
            }

            // 其他小型第三方库合并
            return 'vendor-misc'
          }

          // 应用代码：按入口点自动分包，不手动干预
          // 这样可以避免循环依赖，同时保持合理的 chunk 数量
        }
      }
    }
  },
    server: {
      host: '0.0.0.0',
      port: devPort,
      proxy: {
        '/api': {
          target: backendUrl,
          changeOrigin: true
        },
        '/v1': {
          target: backendUrl,
          changeOrigin: true
        },
        '/setup': {
          target: backendUrl,
          changeOrigin: true
        }
      }
    }
  }
})
