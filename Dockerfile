# =============================================================================
# Sub2API Multi-Stage Dockerfile
# =============================================================================
# Stage 1: Build frontend
# Stage 2: Build Go backend with embedded frontend
# Stage 3: Final minimal image
# =============================================================================

ARG NODE_IMAGE=node:24-alpine
ARG GOLANG_IMAGE=golang:1.26.5-alpine
ARG ALPINE_IMAGE=alpine:3.21
ARG POSTGRES_IMAGE=postgres:18-alpine
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ARG BUILD_NICE_LEVEL=0
ARG FRONTEND_NODE_MAX_OLD_SPACE_SIZE_MB=
ARG FRONTEND_GOMAXPROCS=
ARG FRONTEND_UV_THREADPOOL_SIZE=
ARG GO_BUILD_GOMAXPROCS=
ARG GO_BUILD_PARALLELISM=
ARG GO_BUILD_GOMEMLIMIT=

# -----------------------------------------------------------------------------
# Stage 1: Frontend Builder
# -----------------------------------------------------------------------------
# --platform=$BUILDPLATFORM: the frontend output is JS (arch-neutral), so build
# it on the native host arch instead of under QEMU emulation for the target.
FROM --platform=${BUILDPLATFORM} ${NODE_IMAGE} AS frontend-builder
ARG NPM_CONFIG_REGISTRY
ARG BUILD_NICE_LEVEL
ARG FRONTEND_NODE_MAX_OLD_SPACE_SIZE_MB
ARG FRONTEND_GOMAXPROCS
ARG FRONTEND_UV_THREADPOOL_SIZE

WORKDIR /app/frontend

# Install pnpm. Pin v9 to avoid pnpm v10 approve-builds breaking reproducible Docker builds.
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# Install dependencies first (better caching)
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# Copy frontend source and build
COPY docs/legal/ /app/docs/legal/
COPY frontend/ ./
RUN if [ -n "${FRONTEND_NODE_MAX_OLD_SPACE_SIZE_MB}" ]; then \
        export NODE_OPTIONS="--max-old-space-size=${FRONTEND_NODE_MAX_OLD_SPACE_SIZE_MB} ${NODE_OPTIONS:-}"; \
    fi && \
    if [ -n "${FRONTEND_GOMAXPROCS}" ]; then export GOMAXPROCS="${FRONTEND_GOMAXPROCS}"; fi && \
    if [ -n "${FRONTEND_UV_THREADPOOL_SIZE}" ]; then export UV_THREADPOOL_SIZE="${FRONTEND_UV_THREADPOOL_SIZE}"; fi && \
    nice -n "${BUILD_NICE_LEVEL}" pnpm run build

# -----------------------------------------------------------------------------
# Stage 2: Backend Builder
# -----------------------------------------------------------------------------
# --platform=$BUILDPLATFORM: run the Go toolchain on the native host arch and
# cross-compile to the target arch below. The binary is CGO_ENABLED=0, so this
# is a clean pure-Go cross-compile — no QEMU emulation of go mod download / go
# build (emulated networking here was dropping module fetches with EOF).
FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS backend-builder

# Build arguments for version info (set by CI)
ARG VERSION=
ARG COMMIT=docker
ARG DATE
ARG GOPROXY
ARG GOSUMDB
# Populated by buildx from the --platform target (e.g. linux/amd64).
ARG TARGETOS
ARG TARGETARCH
ARG BUILD_NICE_LEVEL
ARG GO_BUILD_GOMAXPROCS
ARG GO_BUILD_PARALLELISM
ARG GO_BUILD_GOMEMLIMIT

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app/backend

# Copy go mod files first (better caching)
COPY backend/go.mod backend/go.sum ./
# Cache mount keeps the module cache across builds so a transient CDN blip on
# retry resumes instead of re-fetching every zip from scratch.
RUN --mount=type=cache,id=sub2api-gomod,target=/go/pkg/mod \
    go mod download

# Copy backend source first
COPY backend/ ./

# Copy frontend dist from previous stage (must be after backend copy to avoid being overwritten)
COPY --from=frontend-builder /app/backend/internal/web/dist ./internal/web/dist

# Build the binary (BuildType=release for CI builds, embed frontend)
# Version precedence: build arg VERSION > exact git tag > cmd/server/VERSION
RUN --mount=type=cache,id=sub2api-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=sub2api-gobuild,target=/root/.cache/go-build \
    VERSION_VALUE="${VERSION}" && \
    if [ -z "${VERSION_VALUE}" ]; then VERSION_VALUE="$(sh ./scripts/resolve-version.sh)"; fi && \
    DATE_VALUE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    if [ -n "${GO_BUILD_GOMAXPROCS}" ]; then export GOMAXPROCS="${GO_BUILD_GOMAXPROCS}"; fi && \
    if [ -n "${GO_BUILD_GOMEMLIMIT}" ]; then export GOMEMLIMIT="${GO_BUILD_GOMEMLIMIT}"; fi && \
    if [ -n "${GO_BUILD_PARALLELISM}" ]; then export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-p=${GO_BUILD_PARALLELISM}"; fi && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} nice -n "${BUILD_NICE_LEVEL}" go build \
    -tags embed \
    -ldflags="-s -w -X main.Version=${VERSION_VALUE} -X main.Commit=${COMMIT} -X main.Date=${DATE_VALUE} -X main.BuildType=release" \
    -trimpath \
    -o /app/sub2api \
    ./cmd/server

# -----------------------------------------------------------------------------
# Stage 3: PostgreSQL Client (version-matched with docker-compose)
# -----------------------------------------------------------------------------
FROM ${POSTGRES_IMAGE} AS pg-client

# -----------------------------------------------------------------------------
# Stage 4: Final Runtime Image
# -----------------------------------------------------------------------------
FROM ${ALPINE_IMAGE}

# Labels
LABEL maintainer="Wei-Shaw <github.com/Wei-Shaw>"
LABEL description="Sub2API - AI API Gateway Platform"
LABEL org.opencontainers.image.source="https://github.com/Wei-Shaw/sub2api"

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    su-exec \
    libpq \
    zstd-libs \
    lz4-libs \
    krb5-libs \
    libldap \
    libedit \
    && rm -rf /var/cache/apk/*

# Copy pg_dump and psql from the same postgres image used in docker-compose
# This ensures version consistency between backup tools and the database server
COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql
COPY --from=pg-client /usr/local/lib/libpq.so.5* /usr/local/lib/

# Create non-root user
RUN addgroup -g 1000 sub2api && \
    adduser -u 1000 -G sub2api -s /bin/sh -D sub2api

# Set working directory
WORKDIR /app

# Copy binary/resources with ownership to avoid extra full-layer chown copy
COPY --from=backend-builder --chown=sub2api:sub2api /app/sub2api /app/sub2api
COPY --from=backend-builder --chown=sub2api:sub2api /app/backend/resources /app/resources

# Create data directory
RUN mkdir -p /app/data && chown sub2api:sub2api /app/data

# Copy entrypoint script (fixes volume permissions then drops to sub2api)
COPY deploy/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Expose port (can be overridden by SERVER_PORT env var)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://localhost:${SERVER_PORT:-8080}/health || exit 1

# Run the application (entrypoint fixes /app/data ownership then execs as sub2api)
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]
