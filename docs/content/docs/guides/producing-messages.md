---
title: "Producing Messages"
slug: "producing-messages"
description: "How to send messages to queues"
lead: "Learn how to produce messages to queues in Forq using the REST API."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "producing-messages"
weight: 104
toc: true
---

It's not much for you to know here.

## API

See the [API Reference](/documentation-portal/reference/api/) for the complete API documentation.

Messages are produced to a queue using the following endpoint:

```http
POST /queues/{queue}/messages
```

where `{queue}` is the name of the queue to which you want to send messages. If queue does not exist, it will be created automatically.

### Request Body

The request body must be a JSON object with the following fields:

- `content` (string, required): The content of the message as a string, max size is 256 KB.
- `processAfter` (int64, optional): Unix timestamp in milliseconds indicating when the message should be processed. If not provided, the message will be available for processing immediately.

Here is an example request body:

```json
{
  "content": "I am going on an adventure!",
  "processAfter": 1672531199000
}
```

`processAfter` can be set to up to 366 days in the future. Exceeding this limit will result in a 400 Bad Request error. If set to a value in the past, the 400 Bad Request error will be returned.

Please, note that the more delayed messages you have, the more disk memory Forq will use. If you plan to have a lot of delayed messages, consider increasing the disk space available to Forq.

### Authentication

All requests to the Forq API must include an `X-API-Key` header with a valid API key that matches the `FORQ_AUTH_SECRET` environment variable.

### Response

On success, the server will respond with a `204 No Content` status code, indicating that the message was successfully produced to the queue.

Please, note that processing is synchronous, so 204 means that the message has been persisted to the queue DB, and might be available for consumption (if `processAfter` is not set or is in the past).

## Gotchas

### Producing Messages Performance

Usually, it is extremely fast to produce messages to a queue. However, if you are producing a lot of messages in a short period of time, you might hit the disk I/O limits, or SQLite concurrency limits. 
In this case, it might take slightly longer to produce messages.

However, this applies to the VERY high load only. I ran benchmarks with up to 2500 messages per second, and haven't experienced any noticeable delays.

## Cool Tricks

### Recurring Messages

As I mentioned in the [Philosophy Guide](/documentation-portal/docs/guides/philosophy/), Forq can be used as a building block to enable features that it doesn't support natively.

Let's say, you'd like to schedule a message that needs to be processed weekly at the same time. While not possible out of the box, here what can do the trick:

- include the recurrence information in the message content (e.g., the recurrence interval in milliseconds, like `604800000` for weekly recurrence
- produce a message with `processAfter` set to the desired time in the future (e.g., next Monday at 9 AM)
- the consumer will receive this message in a week
- while processing it, the consumer will fetch the recurrence interval from the message content, and produce a new message with `processAfter` set to the current time + recurrence interval (e.g., current time + `604800000` for weekly recurrence).
- repeat until the end of times.

This way, you can implement recurring messages with just a bit of extra logic in the consumer. Easy-peasy!

### Sending Binary Data

Forq messages are just strings. However, you can encode binary data as a string using Base64 encoding.

```pseudocode
stringMessage = base64Encode(binaryData)
sendMessageToQueue(stringMessage, queueName)
```

When consuming the message, decode it back to binary:

```pseudocode
stringMessage = receiveMessageFromQueue(queueName)
binaryData = base64Decode(stringMessage)
```

### What if my message is larger than 256 KB? For example, a file or an image for processing?

A short answer: don't do that. Some might disagree, but I believe that message queues are not the right tool for transferring large files.

Instead, use a dedicated file storage service (e.g., AWS S3, Google Cloud Storage, etc.) to store the file, and send a message with a reference (e.g., URL or file ID) to the file in the message queue.
This way, you can keep your messages small and fast, while still being able to process large files. And the sky is the limit in terms of file size then.
