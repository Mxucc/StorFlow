[English Version](./README.md)

<h1 align="center">
  <br>
  <img src="https://raw.githubusercontent.com/Mxucc/StorFlow-frontend/master/public/static/img/brand/icon-192.png" width="150" alt="StorFlow"/>
  <br>
  StorFlow
  <br>
</h1>

<h4 align="center">你的资源，不止能存。让资源流动，让价值发生。</h4>

<p align="center">
  <a href="https://github.com/Mxucc/StorFlow/pkgs/container/storflow">
    <img src="https://img.shields.io/badge/GHCR-storflow-blue" alt="GHCR"/>
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/badge/license-GPLv3-blue" alt="License"/>
  </a>
</p>

StorFlow 是一套自托管的网盘 + 小店系统：文件存好、看得见，然后把其中值得的东西
卖出去。除网盘能力外，还提供小店、增值服务与公开站点（SEO）。

## 能做什么

**网盘**

- 多存储策略：本地、从机节点、OneDrive、S3 兼容、七牛、阿里云 OSS、腾讯云 COS、华为云 OBS、金山云 KS3、又拍云
- 客户端直传直下，拖拽上传、分片与断点续传
- 全格式在线预览：文档、表格、PDF、Office、Markdown、代码、图片、音视频、压缩包
- 压缩 / 解压 / 批量下载，媒体元数据提取与按标签搜索
- WebDAV 覆盖全部存储策略，桌面客户端与移动端可用
- 分享链接、密码与有效期、只读文件夹
- 多用户多用户组、容量配额、回收站、离线下载（Aria2 / qBittorrent）

**网盘小店**

- 把网盘里的文件夹直接上架成商品，买家付款后自动拿到只读链接文件夹
- 商品介绍是 **Markdown 商品页**：教程、表格、截图、演示视频都能放；素材直接引用
  卖家网盘里的文件，不复制、不额外占空间，替换同名文件后立刻生效
- 介绍素材可见范围三选一：任何人 / 仅登录用户 / 仅已购买用户
- 在线支付（易支付等渠道）、优惠券、返利、订单查询
- 卖家余额、提现申请与审批、退款申请与审批、平台佣金对账
- 店铺装修：主色、标语、公告、背景、banner、logo、页脚、自定义导航

**增值服务**

- 套餐 / 容量包 / 积分 / 兑换码，管理员可配置商品与权益
- 用户侧「商店」页：购买、积分流水、兑换、订单

**公开站点（SEO）**

- `/` 与 `/shop/:slug`、`/shop/:slug/product/:id` 由 Astro 服务端渲染，
  带 canonical、Open Graph、Twitter Card 与 JSON-LD 结构化数据
- `/robots.txt` 与 `/sitemap.xml` 动态生成，含所有上架店铺与商品
- 登录后的控制台、结算、后台仍由内嵌 React SPA 提供，两者按路径分工

## 部署

用预构建镜像（推荐）：

```bash
docker compose up -d
```

镜像由 GitHub Actions 产出：

| 镜像 | 说明 |
| --- | --- |
| `ghcr.io/mxucc/storflow` | 后端（含内嵌前端） |
| `ghcr.io/mxucc/storflow-site` | 公开站点（Astro SSR） |

单独跑后端：

```bash
docker run -d --name storflow \
  -p 5212:5212 -p 6888:6888 -p 6888:6888/udp \
  -v /path/to/data:/storflow/data \
  ghcr.io/mxucc/storflow:latest
```

首次启动后访问 `http://localhost:5212`，按向导完成初始化。

## 从源码构建

前置：Go 1.26+、Node 22+、yarn。

```bash
# 前端是 git 子模块，clone 时必须带 --recursive
git clone --recursive https://github.com/Mxucc/StorFlow.git
cd StorFlow

# 1. 构建前端并打包进 application/statics/assets.zip
chmod +x .build/build-assets.sh
./.build/build-assets.sh 1.0.0

# 2. 编译后端
CGO_ENABLED=0 go build -o storflow .

# 3. 运行
./storflow -c conf.ini server
```

一条命令出 Docker 镜像（不依赖本地 Go / Node）：

```bash
docker build -f Dockerfile.allinone -t storflow:dev .
```

公开站点是独立的 Astro 应用，见 [`site/`](./site)：

```bash
cd site
npm ci
npm run build
PORT=4321 HOST=127.0.0.1 node ./dist/server/entry.mjs
```

然后让后端把公开页面代理过去，临时启动可以用环境变量：

```bash
CR_SITE_ORIGIN=http://127.0.0.1:4321 ./storflow -c conf.ini server
```

想让它跟着配置文件一直生效（两者都设置时环境变量优先）：

```ini
[System]
SiteOrigin = http://127.0.0.1:4321
```

两者都不设置时不会启用代理，首页与店铺页仍由内嵌 SPA 渲染——此时访问 `/`
会被跳到控制台或登录页。详见 [`docs/shop/site-seo.md`](./docs/shop/site-seo.md)。

## 文档

- [公开站点与 SEO](./docs/shop/site-seo.md)
- [商品介绍（Markdown + 网盘素材直连）](./docs/shop/product-page.md)
- [小店前端接口](./docs/shop/frontend-api.md)

## 技术栈

- 后端：[Go](https://golang.org/) + [Gin](https://github.com/gin-gonic/gin) + [ent](https://github.com/ent/ent)
- 前端：[React](https://github.com/facebook/react) + [Redux](https://github.com/reduxjs/redux) + [MUI](https://mui.com/)
- 公开站点：[Astro](https://astro.build/)

## 许可

GPL V3，见 [LICENSE](./LICENSE)。

版权归 StorFlow 的作者、贡献者，以及本项目所基于代码的原作者所有。
其他项目的文档站与 Issue 追踪不代表本项目的支持范围，请使用本仓库的 Issue。
