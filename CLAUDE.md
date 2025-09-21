# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Core Commands
```bash
# Build the application
go build -o forq

# Run the application (requires FORQ_AUTH_SECRET environment variable)
FORQ_AUTH_SECRET=your-secret-token ./forq

# Run database migrations
go run main.go  # migrations run automatically on startup

# Run tests
go test ./...

# Run benchmarks
cd benchmarks && go test -bench=.
```

### Documentation Sites

The project includes two separate Hugo sites:

```bash
# Landing site (marketing)
cd site && npm run dev          # Development server at http://localhost:1313
cd site && npm run build        # Production build
cd site && npm run clean        # Clean build artifacts

# Documentation site (technical docs)
cd docs && npm run dev          # Development server
cd docs && npm run build        # Production build
cd docs && npm run clean        # Clean build artifacts
```

### Docker
```bash
# Build Docker image
docker build -t forq .

# Run with Docker
docker run -e FORQ_AUTH_SECRET=your-secret \
           -e FORQ_API_ADDR=:8080 \
           -e FORQ_UI_ADDR=:8081 \
           -p 8080:8080 -p 8081:8081 \
           forq
```

## Architecture Overview

### Core Components
- **`main.go`**: Application entry point, configuration, and startup
- **`api/`**: JSON REST API endpoints for message operations
- **`ui/`**: HTMX-based web interface for queue management
- **`services/`**: Business logic (messages, queues, sessions, monitoring)
- **`db/`**: SQLite repository and database models
- **`common/`**: Shared models, requests, responses, and constants
- **`jobs/`**: Background tasks (cleanup, metrics collection)
- **`metrics/`**: Prometheus metrics integration (optional)

### Database Design
Uses a single SQLite table (`messages`) for both regular queues and dead letter queues (DLQ). Key design decisions:
- WAL mode enabled for better concurrency
- Integer timestamps (milliseconds) for performance
- UUID v7 for message IDs
- Status codes: 0=ready, 1=processing, 2=failed
- DLQ messages identified by `is_dlq` boolean and queue name suffix `-dlq`

### Configuration
All configuration via environment variables:
- **Required**: `FORQ_AUTH_SECRET` (authentication token)
- **Optional**: `FORQ_DB_PATH` (default: OS-specific based on utils/os.go), `FORQ_ENV` (local|pro), `FORQ_METRICS_ENABLED` (true|false), `FORQ_API_ADDR`, `FORQ_UI_ADDR`, `FORQ_QUEUE_TTL_HOURS`, `FORQ_DLQ_TTL_HOURS`

### Key Design Principles
- Single binary deployment with embedded SQLite
- HTTP/2 preferred for optimal long polling performance
- Simple authentication (single token for all operations)
- Opinionated choices to avoid feature creep
- 256KB message size limit to encourage good architecture
- 5-attempt retry with exponential backoff (1s, 5s, 15s, 30s, 60s)

### API Structure
- **JSON API** (`/api/v1/*`): Programmatic access for producers/consumers
- **HTMX UI** (`/*`): Human-friendly queue management interface
- Two separate servers: API (port 8080) and UI (port 8081) by default

### Message Processing Flow
1. Messages sent to queue via `POST /api/v1/queues/{queue}/messages`
2. Consumers poll via `GET /api/v1/queues/{queue}/messages` (30s long polling)
3. Processing acknowledgment via `POST /api/v1/queues/{queue}/messages/{messageId}/ack`
4. Failed messages retry with exponential backoff, eventually moved to DLQ
5. Background jobs handle cleanup of expired messages and timeout detection

### Metrics (Optional)
When `FORQ_METRICS_ENABLED=true`:
- Prometheus-format metrics at `GET /metrics`
- Separate authentication via `FORQ_METRICS_AUTH_SECRET`
- Real-time API counters and periodic queue depth gauges
- Zero impact on write performance