# StorFlow 后端镜像。
#
# 这个 Dockerfile 只做「打包」：它期望工作目录下已经有编译好的
# `storflow-linux-amd64` / `storflow-linux-arm64` 二进制（CI 里由 build job 产出）。
# 本地单架构构建可以自己 `GOOS=linux GOARCH=amd64 go build -o storflow-linux-amd64 .`。
#
# 想一条命令从源码构建整个镜像（含前端与公开站点），用 Dockerfile.allinone；
# 想构建公开站点（Astro SSR）镜像，用 site/Dockerfile。
FROM alpine:latest

WORKDIR /storflow

RUN apk update \
    && apk add --no-cache tzdata vips-tools ffmpeg libreoffice aria2 supervisor font-noto font-noto-cjk libheif libraw-tools\
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone \
    && mkdir -p ./data/temp/aria2 \
    && chmod -R 766 ./data/temp/aria2

ENV CR_ENABLE_ARIA2=1 \
    CR_SETTING_DEFAULT_thumb_ffmpeg_enabled=1 \
    CR_SETTING_DEFAULT_thumb_vips_enabled=1 \
    CR_SETTING_DEFAULT_thumb_libreoffice_enabled=1 \
    CR_SETTING_DEFAULT_media_meta_ffprobe=1  \
    CR_SETTING_DEFAULT_thumb_libraw_enabled=1

# buildx 每个目标平台会传入对应的 TARGETARCH（amd64 / arm64），
# 因此这里挑同名后缀的二进制，避免多架构镜像里塞进错误架构的可执行文件。
ARG TARGETARCH
COPY .build/aria2.supervisor.conf .build/entrypoint.sh ./
COPY storflow-linux-${TARGETARCH} ./storflow

RUN chmod +x ./storflow \
    && chmod +x ./entrypoint.sh

EXPOSE 5212 443 6888 6888/udp

VOLUME ["/storflow/data"]

ENTRYPOINT ["sh", "./entrypoint.sh"]
