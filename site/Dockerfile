# 公开站点镜像（营销首页 + 小店前台，Astro SSR）。
#
# 该容器只负责渲染 SEO 需要的几个公开页面；Go 后端通过 CR_SITE_ORIGIN 把
# 这些路径的 HTML 请求反向代理到这里（见 middleware/site.go）。
#
# 构建：docker build -t storflow-site ./site
# 数据：CR_API_BASE 指向 Go 后端（容器内通常是 http://storflow:5212）
FROM node:22-alpine AS builder

WORKDIR /app

# 先只复制依赖清单，让 npm ci 的层缓存能复用。
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
RUN npm run build

FROM node:22-alpine

WORKDIR /app
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=4321

# 运行只需要构建产物与生产依赖。
COPY --from=builder /app/package.json /app/package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund && npm cache clean --force

COPY --from=builder /app/dist ./dist

EXPOSE 4321

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD node -e "fetch('http://127.0.0.1:4321/robots.txt').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["node", "./dist/server/entry.mjs"]
