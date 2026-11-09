# syntax=docker/dockerfile:1
# Qubar API 镜像（l0sgAi/qubar#51）：多阶段构建，静态二进制 + distroless nonroot。
#
#   docker build -t qubar .
#   docker buildx build --platform linux/amd64,linux/arm64 -t qubar .
#   docker run -p 8888:8888 \
#     -v "$PWD/configs/config.yaml:/etc/qubar/config.yaml:ro" \
#     -e QUBAR_PGSQL_PASSWORD=... -e QUBAR_SECURITY_DATA_KEY=... qubar
#
# 配置：挂载 /etc/qubar/config.yaml（模板见 configs/config.example.yaml），
# 密钥用 QUBAR_* 环境变量注入（见 pkg/conf/env.go）。镜像内不含任何配置/密钥。

# 构建镜像固定到与 go.mod `go` 指令相同的补丁版本，并锁定 digest（多架构 index），避免浮动标签带入旧工具链（#196）。
# 升级 Go 时：先改 go.mod 的 `go` 指令，再把下面的标签改成同一版本，digest 用
#   docker buildx imagetools inspect golang:<版本> --format '{{json .Manifest.Digest}}'
# 取 index 的 digest（不是某个平台的 manifest digest）。
FROM --platform=$BUILDPLATFORM golang:1.26.10@sha256:cfa27959837144bf4649802ba38e59daffd0dd2a7994aa83daaf113285b0f551 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY pkg ./pkg
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/qubar ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/qubar /qubar
EXPOSE 8888
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
    CMD ["/qubar", "-probe", "http://127.0.0.1:8888/healthz"]
ENTRYPOINT ["/qubar", "-c", "/etc/qubar/config.yaml", "-b", ""]
