#!/bin/bash
set -e
export NODE_OPTIONS="--max-old-space-size=8192"

# This script is used to build the assets for the application.
cd assets
rm -rf build
yarn install --network-timeout 1000000
yarn version --new-version $1 --no-git-tag-version
yarn run build

# Copy the build files to the application directory
cd ../
zip -r - assets/build >assets.zip
mv assets.zip application/statics

# 公开站点（Astro SSR）：营销首页 + 小店前台。
#
# 默认不构建：goreleaser 只发 Go 二进制与后端镜像，站点产物由独立容器运行
# （见 site/Dockerfile 与 .github/workflows/docker.yml 里的 site 镜像 job）。
# 需要在本地一起产出时：
#
#   BUILD_SITE=1 ./.build/build-assets.sh <version>
#
if [ "${BUILD_SITE:-0}" = "1" ]; then
  cd site
  npm ci --no-audit --no-fund
  npm run build
fi
