# Stage 1: Build Win7-compatible Web UI (Chrome >= 109 / Firefox >= 115 ESR)
FROM node:22-alpine AS web-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Stage 2: Build pure-Go multi-DB backend
FROM golang:1.25-bookworm AS backend-builder
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webdb-studio ./cmd/server

# Stage 3: Minimal runtime image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget su-exec \
  && addgroup -g 10001 webdb \
  && adduser -D -u 10001 -G webdb webdb \
  && mkdir -p /app/static /data/meta /data/sqlite \
  && chown -R webdb:webdb /app /data

WORKDIR /app
COPY --from=backend-builder /out/webdb-studio /app/webdb-studio
COPY --from=web-builder /src/web/dist /app/static
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod 0755 /app/docker-entrypoint.sh \
  && chown -R webdb:webdb /app /data

ENV PORT=8080 \
    DATA_DIR=/data \
    SQLITE_DIR=/data/sqlite \
    STATIC_DIR=/app/static

EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/api/health" || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
