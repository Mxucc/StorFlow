# GitHub Actions 自编译 Docker 镜像

`.github/workflows/docker.yml` 负责「从源码编译出可部署的镜像」，推送到 GHCR。

## 触发方式

| 触发 | 结果 |
| --- | --- |
| 推 tag（`v1.2.3` 或 `1.2.3`） | 构建并推送 `<tag>`、`latest`、`sha-<7>` |
| 手动 `workflow_dispatch` | 构建并推送 `sha-<7>`；勾选 `push` 才推送，勾选 `latest` 才更新 latest |
| PR 到 master/main | **只构建不推送**，用来验证构建链路没坏 |

镜像：

| 镜像 | 内容 | Dockerfile |
| --- | --- | --- |
| `ghcr.io/mxucc/storflow` | 后端二进制（内嵌前端 SPA） | `Dockerfile` |
| `ghcr.io/mxucc/storflow-site` | 公开站点（Astro SSR） | `site/Dockerfile` |

## 前置条件（**重要**）

前端是 git 子模块，CI 里靠 `actions/checkout` 的 `submodules: recursive` 拉取。
所以：

1. **`.gitmodules` 必须指向你自己的前端仓库**，也就是
   `https://github.com/Mxucc/StorFlow-frontend.git`。
   如果它还指向上游 `StorFlow-frontend`，CI 编出来的镜像里**没有本项目的
   前端改动**（网盘小店的页面、Markdown 商品介绍编辑器等全部丢失），
   但构建依然会成功 —— 这是最隐蔽的坑。
2. **前端的改动必须先推到那个仓库**。子模块记录的是一个具体 commit，
   本地改了没推，CI 拉到的还是旧代码。

检查与修复：

```bash
# 主仓库：确认子模块地址
git config -f .gitmodules submodule.assets.url

# 子模块：确认 remote 与当前 commit 已经推送
cd assets
git remote -v
git status --short          # 有输出就说明还有没提交的改动
git log --oneline -1        # 这个 commit 必须已经存在于远端
git push origin HEAD:master # 推到 StorFlow-frontend
```

推完回到主仓库，把新的子模块 commit 记下来：

```bash
cd ..
git add assets
git commit -m "chore: bump frontend submodule"
git push
```

## 构建做了什么

`build` job（产出二进制，可下载）：

1. Node 22 + yarn 构建前端 → `assets/build`
2. 打包成 `application/statics/assets.zip`（这是 Go `go:embed` 的目标）
3. Go 1.26 编译后端，`-ldflags` 注入版本号与 commit
4. 上传 `storflow-binary` artifact（保留 7 天）

`docker` job：

1. 下载上一步的二进制
2. buildx + QEMU，构建 `linux/amd64` 与 `linux/arm64` 双平台
3. 后端镜像用 `Dockerfile`（只打包二进制，编译已在 CI 里完成，镜像构建很快）
4. 站点镜像用 `site/Dockerfile`（自包含：`npm ci` + `astro build`）
5. 推送到 GHCR，用 `type=gha` 缓存加速后续构建

## 权限

workflow 需要 `packages: write`（已在文件里声明）。如果推送到 GHCR 报
`denied: permission_denied`，去仓库 **Settings → Actions → General →
Workflow permissions** 确认允许读写，并确认包的可见性设置符合预期。

## 本地自编译

不想用 CI，本地一条命令出后端镜像（含前端，不依赖本地 Go/Node）：

```bash
docker build -f Dockerfile.allinone -t storflow:dev .
```

站点镜像：

```bash
docker build -t storflow-site:dev ./site
```

注意 `Dockerfile.allinone` 需要 `assets/` 子模块内容存在：

```bash
git clone --recursive https://github.com/Mxucc/StorFlow.git
# 或者已经 clone 过：
git submodule update --init --recursive
```

## 与 goreleaser 的分工

`.goreleaser.yaml` 仍然负责「发 Release」：多平台二进制（linux/windows/darwin/freebsd）
与 `.goreleaser.yaml` 里声明的镜像。它会在 `before.hooks` 里跑
`.build/build-assets.sh`，**这个脚本默认不构建 Astro 站点**（站点产物由上面的
`docker` job 单独产出），需要一起产出时：

```bash
BUILD_SITE=1 ./.build/build-assets.sh 1.0.0
```

## 环境变量（部署时）

| 变量 | 属于 | 说明 |
| --- | --- | --- |
| `CR_SITE_ORIGIN` | 后端 | 公开站点地址，如 `http://storflow-site:4321`。不设置就不代理 |
| `CR_API_BASE` | 站点 | 后端 API 地址，容器内如 `http://storflow:5212` |
| `CR_SITE_URL` | 站点 | 访客看到的站点地址，用于 canonical / og:url / sitemap |
