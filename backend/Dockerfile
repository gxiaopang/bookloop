# syntax=docker/dockerfile:1.7

FROM golang:1.27-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 10001 bookloop && \
    adduser -S -D -H -u 10001 -G bookloop bookloop && \
    mkdir -p /app/uploads /app/migrations && \
    chown -R bookloop:bookloop /app

WORKDIR /app
COPY --from=build --chown=bookloop:bookloop /out/server /out/migrate /app/
COPY --chown=bookloop:bookloop migrations/ /app/migrations/

USER bookloop
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=15s --retries=4 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/server"]

