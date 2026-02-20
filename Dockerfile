# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary (pass BUILD_TAGS=edge for lightweight edge build)
ARG BUILD_TAGS=""
RUN CGO_ENABLED=0 GOOS=linux go build \
    -tags "${BUILD_TAGS}" \
    -ldflags="-s -w" \
    -o alfred-ai \
    ./cmd/agent

# Final stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Optional: install Chromium for browser tool support
ARG INSTALL_BROWSER=false
RUN if [ "$INSTALL_BROWSER" = "true" ]; then \
      apk add --no-cache chromium nss freetype harfbuzz; \
    fi

# Create non-root user
RUN addgroup -g 1000 alfredai && \
    adduser -D -u 1000 -G alfredai alfredai

WORKDIR /app

# Copy binary, skills, and workflows from builder
COPY --from=builder /build/alfred-ai /app/alfred-ai
COPY --from=builder /build/skills /app/skills
COPY --from=builder /build/workflows /app/workflows

# Create data directories
RUN mkdir -p /app/data/memory /app/data/sessions /app/data/notes \
    /app/data/cron /app/data/canvas /app/data/workflows /app/data/workspace && \
    chown -R alfredai:alfredai /app

# Switch to non-root user
USER alfredai

# Expose ports (HTTP channel default, Gateway default)
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/alfred-ai", "--version"] || exit 1

ENTRYPOINT ["/app/alfred-ai"]
