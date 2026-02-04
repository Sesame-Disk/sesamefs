# Frontend build stage
FROM node:22-bookworm AS frontend-builder

WORKDIR /app

COPY frontend/package*.json ./
RUN npm ci --legacy-peer-deps

COPY frontend/ .
COPY frontend/.env* ./
RUN if [ ! -f .env ]; then \
    echo "NODE_MAX_MEMORY=4096" > .env && \
    echo "GENERATE_SOURCEMAP=false" >> .env && \
    echo "WEBPACK_PARALLEL_BUILD=true" >> .env && \
    echo "SESAMEFS_API_URL=" >> .env; \
    fi

RUN export $(cat .env | grep -v '^#' | xargs) && \
    NODE_MAX_MEMORY=${NODE_MAX_MEMORY:-4096} && \
    GENERATE_SOURCEMAP=${GENERATE_SOURCEMAP:-false} && \
    WEBPACK_PARALLEL_BUILD=${WEBPACK_PARALLEL_BUILD:-false} && \
    export GENERATE_SOURCEMAP && \
    export WEBPACK_PARALLEL_BUILD && \
    NODE_OPTIONS=--max_old_space_size=$NODE_MAX_MEMORY npm run build

# Go build stage
FROM --platform=$BUILDPLATFORM golang:1.25-trixie AS builder

ARG TARGETOS TARGETARCH

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary for the target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-arm64} go build \
    -ldflags="-w -s" \
    -o sesamefs \
    ./cmd/sesamefs

# Runtime stage
FROM debian:trixie-slim

# Install ca-certificates for HTTPS, tzdata for timezones, wget for healthchecks
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata \
        wget && \
    rm -rf /var/lib/apt/lists/* && \
    apt-get clean

# Create non-root user for security
RUN useradd -r -u 1000 -s /bin/false sesamefs

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/sesamefs .

# Copy config files for development
COPY --from=builder /build/config.docker.yaml ./config.yaml

# Copy frontend build for serving static files (share link views, etc.)
COPY --from=frontend-builder /app/build ./frontend/build

# Use non-root user
USER sesamefs

# Expose API port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O - http://localhost:8080/ping || exit 1

ENTRYPOINT ["/app/sesamefs"]
CMD ["serve"]
