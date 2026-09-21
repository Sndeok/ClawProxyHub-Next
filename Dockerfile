# 三阶段构建：Next 静态导出 → Go 单二进制（含内嵌前端）→ 精简运行时镜像
# 说明：Next 的产物必须先进 web/dist，Go 的 go:embed 才能把它打进二进制，
# 所以前端阶段要跑在 Go 阶段之前，且是同一个构建上下文。

# ---------- 前端：Next.js 静态导出 ----------
FROM node:22-alpine AS web
WORKDIR /src/web-next
RUN corepack enable
COPY web-next/package.json web-next/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY web-next/ ./
# 只跑 next build（导出到 out/），拷进 web/dist 由下一步 embed
RUN pnpm exec next build

# ---------- 后端：Go 编译（CGO 关掉，SQLite 用纯 Go 驱动以外的场景不需要） ----------
FROM golang:1.26-alpine AS api
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY sdk/ ./sdk/
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web-next/out ./web/dist
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/cph ./cmd/cph

# ---------- 运行时 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && mkdir -p /data
ENV CPH_ADDR=:8080 \
    CPH_DATA_DIR=/data \
    TZ=Asia/Shanghai
WORKDIR /data
COPY --from=api /out/cph /usr/local/bin/cph
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/cph"]
