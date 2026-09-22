# 多阶段构建：Next 静态导出（node）→ Go 嵌入编译（go, CGO）→ 运行镜像
# 与上一代 ClawProxyHub 保持一致的基础镜像与 CGO 设置，避免构建环境差异踩坑。

FROM node:22-alpine AS web
WORKDIR /src/web-next
RUN npm install -g pnpm@10
COPY web-next/package.json web-next/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web-next/ ./
# 只跑 next build（静态导出到 out/）；拷进 web/dist 由下一步 embed
RUN pnpm exec next build

FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /src/web-next/out ./web/dist
# mattn/go-sqlite3 需要 CGO
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/cph ./cmd/cph

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata     && rm -rf /var/lib/apt/lists/*

WORKDIR /app/
COPY --from=builder /out/cph ./cph

ENV CPH_ADDR=":8080" \
    CPH_DATA_DIR="/app/data" \
    TZ=Asia/Shanghai
VOLUME ["/app/data"]
EXPOSE 8080

ENTRYPOINT ["./cph"]
