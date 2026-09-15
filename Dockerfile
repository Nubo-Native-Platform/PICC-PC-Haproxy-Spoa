# ==============================================================================
# Build Stage
# ==============================================================================
FROM golang:alpine AS builder

# Install security certificates and build tools
RUN apk add --no-cache ca-certificates git

WORKDIR /app

# Leverage Docker cache by copying go.mod and go.sum first
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source files
COPY . .

# Compile static binary with optimizations and stripped debug symbols
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=1.0.0" \
    -trimpath \
    -o /out/spoa .

# ==============================================================================
# Runtime Stage (CNCF Compliant, Non-Root Unprivileged Runtime)
# ==============================================================================
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 10001 -S appgroup \
    && adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder --chown=10001:10001 /out/spoa /app/spoa

# Run as non-root user
USER 10001:10001

# Port 9000: HAProxy SPOE Binary TCP Protocol
# Port 8080: Kubernetes HTTP Health / Liveness Probe
EXPOSE 9000 8080

# Container healthcheck using HTTP health endpoint
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/spoa"]