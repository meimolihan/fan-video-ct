# fan-video-ct: 视频剪切工具
# 前端为静态网页，编译期直接内嵌进二进制（无 npm/Node 阶段）。
# 运行镜像内置 ffmpeg/ffprobe，支撑流式剪切（copy/reencode）与封面帧捕获。

# ---------- 构建阶段 ----------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG FCT_VERSION=0.1.0
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /app
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags="-s -w -X github.com/meimolihan/fan-video-ct/internal/version.Version=${FCT_VERSION}" \
      -o fan-video-ct .

# ---------- 运行阶段 ----------
FROM alpine:3.20
ARG FCT_VERSION=0.1.0

RUN apk add --no-cache \
      ffmpeg \
      tzdata \
      ca-certificates \
      su-exec \
    && ffmpeg -version | head -n 1 \
    && ffprobe -version | head -n 1

RUN addgroup -S fct && adduser -S fct -G fct
WORKDIR /app
COPY --from=builder /app/fan-video-ct /usr/local/bin/fan-video-ct
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /data \
    && chown -R fct:fct /data /app

ENV FCT_APP_PORT=8788
ENV FCT_APP_DATA_DIR=/data
ENV FCT_APP_WEB_DIR=
ENV FCT_LOGGING_LEVEL=info
ENV FCT_VERSION=${FCT_VERSION}
ENV TZ=Asia/Shanghai

EXPOSE 8788

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://localhost:8788/api/health || exit 1

# root 进入 entrypoint（建目录 + 授属主给 fct），随后 su-exec 降权运行应用
USER root
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]