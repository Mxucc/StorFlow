[中文版本](./README_zh-CN.md)

<h1 align="center">
  <br>
  <img src="https://raw.githubusercontent.com/Mxucc/StorFlow-frontend/master/public/static/img/brand/icon-192.png" width="150" alt="StorFlow"/>
  <br>
  StorFlow
  <br>
</h1>

<h4 align="center">Your resources, more than storage. Let them flow, let them be worth something.</h4>

<p align="center">
  <a href="https://github.com/Mxucc/StorFlow/pkgs/container/storflow">
    <img src="https://img.shields.io/badge/GHCR-storflow-blue" alt="GHCR"/>
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/badge/license-GPLv3-blue" alt="License"/>
  </a>
</p>

StorFlow is a self-hosted drive **and** a storefront: keep your files, see them
anywhere, and sell the ones worth selling. It ships with a shop, value-added
services and an SEO-friendly public site.

## What it does

**Drive**

- Storage policies: local, remote node, OneDrive, S3 compatible, Qiniu Kodo, Aliyun OSS, Tencent COS, Huawei Cloud OBS, Kingsoft KS3, Upyun
- Client-side upload/download, drag & drop, chunked and resumable transfers
- In-browser preview for documents, spreadsheets, PDF, Office, Markdown, code, images, audio/video and archives
- Compress / extract / batch download, media metadata extraction, tag search
- WebDAV across every storage policy; desktop and mobile clients
- Share links with password and expiry, read-only folders
- Multi-user with groups and quotas, recycle bin, offline download (Aria2 / qBittorrent)

**Shop**

- List a drive folder as a product; buyers receive a read-only linked folder automatically
- Product pages are **Markdown**: tutorials, tables, screenshots, demo videos.
  Images and videos are embedded straight from the seller's drive — nothing is
  copied, and replacing a file in the drive updates the page immediately
- Asset visibility per product: public / signed-in users / buyers only
- Online payment (epay and friends), coupons, referral rewards, order lookup
- Seller balance, withdrawal requests and review, refunds, commission ledger
- Storefront decoration: primary colour, slogan, notice, background, banner, logo, footer, custom links

**Value-added services**

- Plans, storage packs, points and gift codes, all configured by the administrator
- A user-facing store for purchases, point history, redemption and orders

**Public site (SEO)**

- `/`, `/shop/:slug` and `/shop/:slug/product/:id` are server-rendered by Astro with
  canonical URLs, Open Graph, Twitter Cards and JSON-LD
- `/robots.txt` and `/sitemap.xml` are generated dynamically from live shops and products
- The signed-in console, checkout and admin panel stay in the embedded React SPA;
  the two split work by path

## Deploy

Prebuilt images (recommended):

```bash
docker compose up -d
```

Images are published by GitHub Actions:

| Image | Description |
| --- | --- |
| `ghcr.io/mxucc/storflow` | Backend (with the embedded frontend) |
| `ghcr.io/mxucc/storflow-site` | Public site (Astro SSR) |

Backend only:

```bash
docker run -d --name storflow \
  -p 5212:5212 -p 6888:6888 -p 6888:6888/udp \
  -v /path/to/data:/storflow/data \
  ghcr.io/mxucc/storflow:latest
```

Open `http://localhost:5212` and finish the setup wizard.

## Build from source

Requires Go 1.26+, Node 22+ and yarn.

```bash
# The frontend is a git submodule: clone with --recursive
git clone --recursive https://github.com/Mxucc/StorFlow.git
cd StorFlow

# 1. Build the frontend into application/statics/assets.zip
chmod +x .build/build-assets.sh
./.build/build-assets.sh 1.0.0

# 2. Build the backend
CGO_ENABLED=0 go build -o storflow .

# 3. Run
./storflow -c conf.ini server
```

Or produce a Docker image in one command, without a local Go/Node toolchain:

```bash
docker build -f Dockerfile.allinone -t storflow:dev .
```

The public site is a separate Astro app in [`site/`](./site):

```bash
cd site
npm ci
npm run build
PORT=4321 HOST=127.0.0.1 node ./dist/server/entry.mjs
```

Then let the backend proxy the public pages to it — either through the environment:

```bash
CR_SITE_ORIGIN=http://127.0.0.1:4321 ./storflow -c conf.ini server
```

or, so restarts keep working, in `conf.ini` (the env var wins when both are set):

```ini
[System]
SiteOrigin = http://127.0.0.1:4321
```

Without either one no proxy is installed and the home page and storefront keep
being rendered by the embedded SPA — which redirects `/` to the console or the
sign-in page. See [`docs/shop/site-seo.md`](./docs/shop/site-seo.md).

## Documentation

- [Public site and SEO](./docs/shop/site-seo.md)
- [Product pages (Markdown + drive assets)](./docs/shop/product-page.md)
- [Shop frontend API](./docs/shop/frontend-api.md)

## Stacks

- Backend: [Go](https://golang.org/) + [Gin](https://github.com/gin-gonic/gin) + [ent](https://github.com/ent/ent)
- Frontend: [React](https://github.com/facebook/react) + [Redux](https://github.com/reduxjs/redux) + [MUI](https://mui.com/)
- Public site: [Astro](https://astro.build/)

## License

GPL V3, see [LICENSE](./LICENSE).

Copyright belongs to the StorFlow authors, contributors, and the authors of the
code this project derives from. Documentation and issue trackers of any other
project do not cover this one; use this repository's issues instead.
