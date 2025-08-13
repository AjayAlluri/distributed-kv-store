# Multi-stage build for distributed key-value store
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/cluster cmd/cluster/main.go

# Final stage - minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 kvstore && \
    adduser -D -s /bin/sh -u 1001 -G kvstore kvstore

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/bin/cluster /app/cluster

# Copy configuration files
COPY --from=builder /app/config/ /app/config/

# Create data directory with proper permissions
RUN mkdir -p /app/data && \
    chown -R kvstore:kvstore /app

# Switch to non-root user
USER kvstore

# Expose ports
# HTTP API ports: 8081-8083
# Raft communication ports: 9001-9003
EXPOSE 8081 8082 8083 9001 9002 9003

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:8081/health || exit 1

# Default command
CMD ["./cluster", "--config", "config/multi-node-cluster.yaml"]