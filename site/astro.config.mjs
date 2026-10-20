import node from "@astrojs/node";
import { defineConfig } from "astro/config";

/**
 * StorFlow 公开站点（营销首页 + 小店前台）。
 *
 * 只负责「SEO 需要被爬虫直接读到的那几页」：`/`、`/shop/:slug`、`/shop/:slug/product/:id`。
 * 登录后的控制台、结算、管理后台仍然由 Go 内嵌的 React SPA 提供；
 * Go 侧按路径把 HTML 请求反向代理到这里（见 middleware/site.go）。
 */
export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
  trailingSlash: "ignore",
  compressHTML: true,
  build: {
    // 资源放进 /site-assets/，避免和 SPA 的 /assets/ 冲突。
    assets: "site-assets",
  },
  vite: {
    build: {
      assetsInlineLimit: 2048,
    },
  },
});
