FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o 9router-gateway ./cmd/gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata sqlite curl
WORKDIR /app
COPY --from=builder /app/9router-gateway /app/9router-gateway
RUN mkdir -p /app/data
EXPOSE 20129
CMD ["/app/9router-gateway"]
