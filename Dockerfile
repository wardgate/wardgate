# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o wardgate ./cmd/wardgate

# Runtime stage
FROM alpine:3.21

WORKDIR /app

# Install ca-certificates for TLS connections to upstream services
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 wardgate

# Copy binary from builder
COPY --from=builder /app/wardgate /app/wardgate

# Copy presets
COPY --from=builder /app/presets /app/presets

# Default config location
VOLUME ["/app/config"]

# Switch to non-root user
USER wardgate

EXPOSE 8080

ENTRYPOINT ["/app/wardgate"]
CMD ["-config", "/app/config/config.yaml", "-env", "/app/config/.env"]
