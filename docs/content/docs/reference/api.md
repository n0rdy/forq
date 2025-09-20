---
title: "API Reference"
description: "Complete Forq API documentation"
lead: "RESTful API endpoints for message queue operations."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-14T20:30:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "reference"
    identifier: "api"
weight: 200
toc: true
---

{{< callout context="tip" title="Interactive API Documentation" icon="outline/external-link" >}}
**[View Interactive API Documentation →](/docs/api/)**

Complete OpenAPI specification with interactive examples, request/response details, and authentication information.
{{< /callout >}}

# API TLDR

## Authentication

All API endpoints require authentication via the `X-API-Key` header:

```http
X-API-Key: your-secret-token
```

## Message Operations

### Produce Message

Send a message to a queue.

```http
POST /api/v1/queues/{queue}/messages
```

**Request Body:**

```json
{
  "content": "Your message content (256KB max)",
  "processAfter": 1757875397418 // Optional: delay processing
}
```

**Response:**

204 No Content empty body

### Consume Message

Long-poll for the next available message (30s timeout).

```http
GET /api/v1/queues/{queue}/messages
```

**Response:**

```json
{
  "id": "0199164b-4dea-78d9-9b4c-c699d5037962",
  "content": "Your message content (256KB max)"
}
```

### Acknowledge Message

Mark a message as successfully processed.

```http
POST /api/v1/queues/{queue}/messages/{messageId}/ack
```

**Response:**

204 No Content empty body

### Negative Acknowledge

Mark a message as failed (will retry with backoff).

```http
POST /api/v1/queues/{queue}/messages/{messageId}/nack
```

**Response:**

204 No Content empty body

## Error Handling

All endpoints return appropriate HTTP status codes:

- `200`/`204` - Success
- `400`       - Bad Request (invalid JSON, content too large)
- `401`       - Unauthorized (missing or invalid API key)
- `404`       - Not Found (queue or message not found)
- `500`       - Internal Server Error

## Rate Limits

Forq does not implement built-in rate limiting. Use nginx, Cloudflare, or load balancers for rate limiting in production.