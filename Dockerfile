# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary (static build with pure Go SQLite)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags '-w -s -extldflags "-static"' -o forq .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create app user
RUN addgroup -g 1001 -S forq && \
    adduser -u 1001 -S forq -G forq

# Database will be created in user home directory based on OS conventions
# Linux: /home/forq/.local/share/forq/forq.db

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/forq .

# Copy runtime assets
COPY --from=builder /app/db/migrations ./db/migrations
#COPY --from=builder /app/ui/static ./ui/static
COPY --from=builder /app/ui/templates ./ui/templates

# Change ownership
RUN chown -R forq:forq /app

# Switch to non-root user
USER forq

# Expose ports
EXPOSE 8080 8081

# Set default environment variables
ENV FORQ_ENV=pro

# Run the binary
CMD ["./forq"]
