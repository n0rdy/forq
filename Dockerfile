# Runtime stage (GoReleaser provides the binary)
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

# Copy pre-built binary (provided by GoReleaser)
COPY forq .

# Copy runtime assets
COPY db/migrations ./db/migrations
COPY ui/templates ./ui/templates

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
