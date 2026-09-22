# 多阶段构建：仪表盘（node）→ 核心（go）→ 运行镜像
# 目录结构与上一代 ClawProxyHub 一致：前端源码、构建产物、embed.go 都在 web/。
#
# 构建代理（需要时把下面每个阶段的 ENV 块取消注释即可）：
#   http://192.168.31.66:7893

# =========================
# Web 前端构建
# =========================
FROM node:22-alpine AS web

# ENV HTTP_PROXY=http://192.168.31.66:7893 \
#     HTTPS_PROXY=http://192.168.31.66:7893 \
#     ALL_PROXY=http://192.168.31.66:7893 \
#     http_proxy=http://192.168.31.66:7893 \
#     https_proxy=http://192.168.31.66:7893 \
#     all_proxy=http://192.168.31.66:7893

WORKDIR /src/web

RUN npm install -g pnpm@10

COPY web/package.json web/pnpm-lock.yaml ./

RUN pnpm install --frozen-lockfile

COPY web/ .

# next build 静态导出到 out/，再由 scripts/export-to-embed.mjs 同步到 web/dist
RUN pnpm build

# =========================
# Go 核心构建
# =========================
FROM golang:1.26 AS builder

# ENV HTTP_PROXY=http://192.168.31.66:7893 \
#     HTTPS_PROXY=http://192.168.31.66:7893 \
#     ALL_PROXY=http://192.168.31.66:7893 \
#     http_proxy=http://192.168.31.66:7893 \
#     https_proxy=http://192.168.31.66:7893 \
#     all_proxy=http://192.168.31.66:7893

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

COPY --from=web /src/web/dist ./web/dist

# mattn/go-sqlite3 需要 CGO
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/cph \
    ./cmd/cph

# =========================
# 最终运行镜像
# =========================
FROM debian:bookworm-slim

# ENV HTTP_PROXY=http://192.168.31.66:7893 \
#     HTTPS_PROXY=http://192.168.31.66:7893 \
#     ALL_PROXY=http://192.168.31.66:7893 \
#     http_proxy=http://192.168.31.66:7893 \
#     https_proxy=http://192.168.31.66:7893 \
#     all_proxy=http://192.168.31.66:7893

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app/

COPY --from=builder /out/cph ./cph

ENV CPH_ADDR=":8080" \
    CPH_DATA_DIR="/app/data" \
    TZ=Asia/Shanghai

VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["./cph"]
