# 公开站点（Astro SSR）：首页介绍页与小店前台

小店的店铺页与首页需要被搜索引擎直接读到完整的 HTML（标题、描述、Open Graph、
结构化数据），而内嵌的 React SPA 是纯客户端渲染，不满足这个要求。因此这里额外
提供一份 Astro 服务端渲染的公开站点，Go 后端把「需要 SEO 的那几条路径」反代过去。

```
浏览器 ──> Go 后端 :5212 ──(仅公开 HTML)──> Astro :4321 ──> Go 后端 /api/v4/*
                    └──(其余路径)──> 内嵌 SPA
```

## 覆盖的页面

| 路径 | 渲染者 | SEO |
| --- | --- | --- |
| `/` | Astro | 介绍页，`WebSite` + `FAQPage` 结构化数据 |
| `/shop/:slug` | Astro | 店铺页，`Store` + `ItemList` + `BreadcrumbList` |
| `/shop/:slug/product/:id` | Astro | 商品页，`Product`（含价格、库存状态） |
| `/robots.txt`、`/sitemap.xml` | Astro | 动态生成，sitemap 含所有上架店铺与商品 |
| `/session`、`/home`、`/admin/*`、`/store` | SPA | — |
| `/shop/manage`、`/shop/purchases`、`/shop/order/*` | SPA | — |
| `/api/*` | Go | — |

判定逻辑见 `middleware/site.go` 的 `shouldProxy`：只代理 GET/HEAD 且 `Accept`
包含 `text/html` 的请求；XHR / fetch（`Accept: application/json`）仍然直连后端，
所以页内软导航、下单接口都不受影响。

## 本地开发

```bash
# 1. 起站点（默认读 CR_API_BASE，未设置时为 http://127.0.0.1:5212）
cd site
npm ci
npm run build          # 生产构建，产物在 site/dist
PORT=4321 HOST=127.0.0.1 node ./dist/server/entry.mjs
# 或者开发态：npm run dev

# 2. 让 Go 后端代理过去
cd ..
CR_SITE_ORIGIN=http://127.0.0.1:4321 ./storflow -c data/conf.ini server
```

站点被 Go 反代时，`Astro.url.origin` 是 `127.0.0.1:4321`，不能直接拿来做
canonical。`site/src/lib/format.ts` 的 `resolveOrigin()` 按以下优先级取访客域名：

1. `CR_SITE_URL`（显式配置，最稳）
2. `X-Forwarded-Host` + `X-Forwarded-Proto`（由 `middleware/site.go` 透传）
3. 请求的 `Host` 头
4. `Astro.url.origin`（兜底）

## 环境变量

| 变量 | 属于 | 说明 |
| --- | --- | --- |
| `CR_SITE_ORIGIN` | Go 后端 | 站点地址，例如 `http://site:4321`。也可以写在 `conf.ini` 的 `[System] SiteOrigin`（环境变量优先）。**两者都不设置就不代理**，`/` 会落回 SPA 并跳转到控制台/登录页 |
| `CR_API_BASE` | Astro | 后端 API 地址，容器内通常是 `http://storflow:5212` |
| `CR_SITE_URL` | Astro | 访客看到的站点地址（如 `https://pan.example.com`），用于 canonical / og:url / sitemap。不设置则取转发头 |
| `PORT` / `HOST` | Astro | 监听地址，默认 `4321` / `127.0.0.1`（容器内设为 `0.0.0.0`） |

## Docker 部署

`docker-compose.yml` 已经包含 `storflow-site` 服务，用的是 GHCR 上的预构建镜像：

```bash
docker compose up -d
```

想从源码构建，把 compose 里的 `image` 换成 `build: ./site` 即可（或直接
`docker build -t storflow-site:dev ./site`）。镜像由
`.github/workflows/docker.yml` 产出，详见 [GitHub Actions 自编译 Docker](../build/docker-ci.md)。

`storflow-site` 容器不对外暴露端口，只由 `storflow` 容器访问；上线后把
`CR_SITE_URL` 改成真实域名，避免 sitemap 里出现内网地址。

## 回退行为

站点没启动、超时或返回错误时，`middleware/site.go` 会标记本次请求失败并继续走
gin 链，由内嵌 SPA 渲染（也就是旧行为）。因此 Astro 挂了不会导致首页 502。

## 素材复用

图片（logo、banner、默认商品图、favicon）只在 SPA 的 `assets/public/static/img/`
维护一份。站点侧 `/static/*` 走 `src/pages/static/[...path].ts` 302 到主站，
不做镜像。

## Service Worker 必须放行这些路径（踩过的坑）

内嵌 SPA 注册了 PWA Service Worker。**只要 precache 里有 `index.html`，访问 `/`
就会被 workbox-precaching 的「目录索引」匹配（对以 `/` 结尾的请求自动补
`index.html` 去找缓存条目）直接命中缓存的 SPA 壳**，这一步发生在
`NavigationRoute` 的 denylist 之前，所以加多少 denylist 都拦不住 ——
表现就是已登录用户怎么点都回不到官网首页，只能看到控制台或登录页。

因此 `assets/vite.config.ts` 里做了两件事：

1. `globIgnores` 排除 `index.html`，并显式 `navigateFallback: undefined`
   （插件默认会补 `"index.html"`，index.html 不在 precache 里时
   `createHandlerBoundToURL` 会抛 `non-precached-url`，让 SW 安装直接失败）；
2. 导航请求统一交给 `runtimeCaching` 的 `NetworkFirst`：
   在线时始终取服务端最新 HTML（官网首页 / 小店前台来自 Astro，控制台 / 登录页
   来自内嵌 SPA），离线时才回退到缓存。

改完这些，用 `assets/build/sw.js` 确认 `NavigationRoute` 与 `index.html`
都不再出现即可。

## 双向入口

官网首页与登录后的控制台必须能互相到达：

- 登录页（`HeadlessFrame`）右上角有「官网首页」链接；
- 控制台用户菜单（`UserAction`）里有「官网首页」；
- 官网首页检测到 localStorage 里存在未过期 token 时，把「开始使用 / 登录 / 注册」
  换成「进入控制台」。

三者都必须用整页跳转（`window.location.assign` / 原生 `<a>`）：SPA 内软导航到
`/` 会被路由表的 `HomeRedirect` 接管，只会回到 `/home` 或 `/session`。

同理 `App.tsx` 的 `checkBundleFreshness` 探测的是 `/session` 而不是 `/` ——
`/` 现在返回 Astro HTML，里面没有 SPA 入口包引用。
