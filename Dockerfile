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

FROM --platform=$BUILDPLATFORM golang:1.25 AS build
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
