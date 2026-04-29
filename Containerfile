# Stage 1: 构建 Go 二进制
FROM docker.io/library/golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /scnet-server ./cmd/scnet-server

# Stage 2: 最小运行时（内核 WG 不依赖 userspace go 库）
FROM docker.io/library/alpine:3.21
RUN apk add --no-cache wireguard-tools iproute2 ca-certificates curl
COPY --from=backend /scnet-server /usr/local/bin/scnet-server
COPY migrations/ /etc/scnet/migrations/

EXPOSE 8080 51820/udp

ENTRYPOINT ["/usr/local/bin/scnet-server", "-config", "/etc/scnet/config.yaml"]
