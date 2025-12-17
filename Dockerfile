# Build stage
FROM golang:1.24 AS builder

# Install build dependencies
# Install build dependencies
RUN apt-get update && apt-get install -y ca-certificates git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .



# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'github.com/verikod/hector.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)' -X 'github.com/verikod/hector.GitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)'" -o hector ./cmd/hector

# Final stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 -S hector && \
    adduser -u 1001 -S hector -G hector

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/hector .

# Copy configuration examples
COPY --from=builder /app/configs ./configs

# Change ownership to non-root user
RUN chown -R hector:hector /app

# Switch to non-root user
USER hector

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
CMD ["./hector", "serve", "--config", "configs/hector.yaml"]
