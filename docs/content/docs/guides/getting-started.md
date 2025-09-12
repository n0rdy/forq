---
title: "Getting Started"
description: "Quick start guide for Forq SQLite Queue Service"
lead: "Get Forq up and running in minutes with this quick start guide."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "getting-started"
weight: 100
toc: true
---

## Quick Start

Forq is a simple single-binary message queue on top of SQLite, designed for small to medium workloads.

### Prerequisites

- Go 1.25+ (for building from source)
- Or use Docker

### Installation

#### Build from Source

```bash
git clone https://github.com/n0rdy/forq.git
cd forq
go build -o forq ./main.go
```

### Configuration

Forq uses environment variables for configuration:

```bash
# Required
export FORQ_AUTH_SECRET=your-auth-secret                # to use for API and Admin UI authentication

# Optional
export FORQ_ENV=pro                                     # local|pro (default: pro)
export FORQ_METRICS_ENABLED=false                       # true|false (default: false)
export FORQ_METRICS_AUTH_SECRET=your-metrics-secret     # true|false (default: false)
export FORQ_QUEUE_TTL_HOURS=24                          # Default: 24 hours
export FORQ_DLQ_TTL_HOURS=168                           # Default: 168 hours (7 days)
export FORQ_API_ADDR=localhost:8080                     # Default: localhost:8080
export FORQ_UI_ADDR=localhost:8081                      # Default: localhost:8081
```

### Running Forq

```bash
./forq
# Server starts on :8080 (API) and :8081 (UI) by default
```

### First Message

Send your first message:

```bash
curl -X POST http://localhost:8080/api/v1/queues/emails/messages \
  -H "X-API-Key: your-auth-secret" \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello, World!"}'
```

where `emails` is the queue name. If the queue does not exist, it will be created automatically.

Receive the message:

```bash
curl -X GET http://localhost:8080/api/v1/queues/emails/messages \
  -H "X-API-Key: your-auth-secret"
```

Acknowledge the message:

```bash
curl -X POST http://localhost:8080/api/v1/queues/emails/messages/{message_id}/ack \
  -H "X-API-Key: your-auth-secret"
```

where `{message_id}` is the `id` of the message received in the previous step.

## Next Steps

- [Specification](/docs/guides/specification/) - Overview of Forq features and design, do check it out!
- [SDKs](/docs/guides/sdks/) - Client libraries for various languages
- [API Reference](/docs/reference/api/) - Complete API documentation
- [Configuration](/docs/reference/configuration/) - All configuration options
- [Metrics](/docs/guides/metrics/) - Setting up monitoring with Prometheus
- [Web UI Guide](/docs/guides/web-ui/) - Using the admin interface
- [Forq Internals](/docs/guides/internals/) - How (and why) Forq works under the hood for fellow nerds
- [FAQ](/docs/guides/faq/) - Frequently asked questions
