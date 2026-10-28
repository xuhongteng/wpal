# 多阶段构建：编译阶段带 Go 工具链，运行阶段只留静态二进制。
#
# 产物基于 scratch，最终镜像只有二进制 + CA 证书，约 12 MB。

# ---- build stage ----
FROM golang:1.23-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
ARG TARGETOS
ARG TARGETARCH

# 默认走国内 Go 模块代理；proxy.golang.org 在部分网络环境下不可达。
# 需要直连官方源时可传 --build-arg GOPROXY=https://proxy.golang.org,direct
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB} \
    GOFLAGS=-mod=mod \
    CGO_ENABLED=0

WORKDIR /src

# 依赖层单独缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath \
      -ldflags "-s -w \
        -X mon/internal/version.Version=${VERSION} \
        -X mon/internal/version.Commit=${COMMIT} \
        -X mon/internal/version.Date=${DATE}" \
      -o /out/wpal ./cmd/wpal

# ---- runtime stage ----
FROM scratch

LABEL org.opencontainers.image.title="Wpal" \
      org.opencontainers.image.description="Wpal - 轻量级跨平台服务器资源监控看板：单个静态二进制、零运行时依赖。" \
      org.opencontainers.image.source="https://github.com/xuhongteng/wpal" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=builder /out/wpal /wpal
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# 数据目录（环形历史文件落在这里）
VOLUME ["/data"]

EXPOSE 8891

# 默认只监听回环；容器网络用 host 时与宿主机一致
ENTRYPOINT ["/wpal"]
CMD ["--listen", "127.0.0.1:8891", "--data-dir", "/data"]
