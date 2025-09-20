---
title: "Consuming Messages"
description: "How to receive messages from queues"
lead: "Learn how to consume messages from queues in Forq using the REST API."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "consuming-messages"
weight: 104
toc: true
---

Consuming messages from Forq is a two-step approach: fetch the message, and then acknowledge it on successful processing, or nacknowledge it on failure. That's it, now you know everything! =)

## API

See the [API Reference](/documentation-portal/reference/api/) for the complete API documentation.

### Consume Message

Messages are consumed from a queue using the following endpoint:

```http
GET /queues/{queue}/messages
```

where `{queue}` is the name of the queue from which you want to receive messages.

Forq uses long-polling for this endpoint with the max timeout of 30 seconds. If there are no messages available for processing, the server will hold the request open and retrying to fetch a message until either a message becomes available or the timeout is reached.

Therefore, consider two things:

- make sure your HTTP Client uses a long enough timeout (at least 35 seconds to account for network latency), so the requests are not cancelled prematurely
- prefer HTTP2, as its multiplexing capabilities will allow you to have multiple long-polling requests in-flight simultaneously over a single connection

Forq uses FIFO ordering for message delivery, so expect to receive the oldest available messages first.

#### Authentication

All requests to the Forq API must include an `X-API-Key` header with a valid API key that matches the `FORQ_AUTH_SECRET` environment variable.

#### Response

On success, the server will respond with a `200 OK` status code and a JSON object representing the message. If there are no messages available for processing, the server will respond with a `204 No Content` status code.

```json
{
  "id": "01995e00-ea5e-74ba-9e7b-aadd93ec3618", // UUID v7 format
  "content": "I am going on an adventure!"
}
```

### Acknowledge Message

Once you have successfully processed a message, you must acknowledge it using the following endpoint:

```http
POST /queues/{queue}/messages/{messageId}/ack
```

#### Authentication

All requests to the Forq API must include an `X-API-Key` header with a valid API key that matches the `FORQ_AUTH_SECRET` environment variable.

#### Response

On success, the server will respond with a `204 No Content` status code, indicating that the message was successfully acknowledged and removed from the queue.

### Nacknowledge Message

If you were unable to process a message, you can nacknowledge it using the following endpoint:

```http
POST /queues/{queue}/messages/{messageId}/nack
```

#### Authentication

All requests to the Forq API must include an `X-API-Key` header with a valid API key that matches the `FORQ_AUTH_SECRET` environment variable.

#### Response

On success, the server will respond with a `204 No Content` status code, indicating that the message was successfully nacknowledged and made available for processing again.

## Gotchas

### Consuming Messages Performance

Usually, it is extremely fast to consume a message from a queue. However, if there are a lot of consumers trying to fetch messages in a short period of time, you might hit the disk I/O limits, or SQLite concurrency limits.
In this case, it might take slightly longer to consume, but not above the long-polling timeout of 30 seconds.

However, this applies to the VERY high load only. I ran benchmarks with up to 2500 messages per second, and haven't experienced any noticeable delays.

### Max Processing Time

You have max 5 minutes to process a message and ack/nack it. 
If you exceed this limit, the message will be considered stale, and Forq will nacknowledge it automatically, 
making it available for processing again with a backoff delay (1s, 5s, 15s, 30s, 60s) if it hasn't exceeded the retry limit (5).
Otherwise, for the standard queues, it will be moved to DLQ, and for DLQs, it will be deleted permanently.

This is a potential source of duplicate message processing, so make sure to call ack/nack within the max processing time, and implement idempotency in your message processing logic (if possible).

### Consuming From DLQ

DLQs are just like standard queues, so you can consume messages from them in the same way.

Please, note that there is no DLQ for DLQs, so once a message exceeds the retry limit in a DLQ, it will be deleted permanently.

### TTL Expiry

If message is not consumed within the TTL (24 hours by default for standard queues, and 7 days for DLQs), it will be:
- for standard queues, moved to DLQ
- for DLQs, deleted permanently

TTLs are configurable via environment variables, check [Configurations](/documentation-portal/docs/guides/configurations/) for more details.
