---
title: "Forq SDKs"
slug: "sdks"
description: "Overview of available Forq SDKs for various programming languages."
lead: "Client libraries for easy integration with Forq."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-14T20:30:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "reference"
    identifier: "sdks"
weight: 201
toc: true
---

I implemented simple Forq SDKs for the ecosystems that I use most often, which is:
- Go
- Java
- TypeScript

Those are very simple, and basically just wrap the HTTP API. 

If your platform of choice is not listed here, you can generate the client code using the [Forq OpenAPI specification](https://github.com/n0rdy/forq/blob/main/openapi.yaml),
or just use the HTTP API directly. The whole API consists of 4 endpoint and 3 models, no big deal.

## Go SDK

The Go SDK is available at [GitHub](https://github.com/n0rdy/forq-sdk-go)

```bash
go get github.com/n0rdy/forq-sdk-go
```

### Producer

You can create a new producer by providing HTTP client, Forq server URL and auth secret:

```go
httpClient := &http.Client{} // add necessary timeouts, etc., DO NOT use http.DefaultClient as it is not safe for production
forqURL := "http://localhost:8080"
authSecret := "your-auth-secret-min-32-chars-long"

p := producer.NewForqProducer(httpClient, forqURL, authSecret)
```

You can then use the producer to send messages:

```go
queueName := "my-queue"
newMessage := api.NewMessageRequest{
    Content: "I am going on an adventure!",
    ProcessAfter: time.Now().Add(time.Hour).UnixMilli(), // optional: deliver in 1 hour; omit for immediate delivery
}

err := p.Produce(context.Background(), newMessage, queueName)
```

### Consumer

You can create a new consumer by providing HTTP client, Forq server URL and auth secret:

```go
httpClient := &http.Client{} // add necessary timeouts, etc., DO NOT use http.DefaultClient as it is not safe for production
forqURL := "http://localhost:8080"
authSecret := "your-auth-secret-min-32-chars-long"

c, err := consumer.NewForqConsumer(httpClient, forqURL, authSecret)
// err is possible here if the provided HTTP Client timeout is shorter than 35 seconds (30 seconds long polling + 5 seconds buffer) 
```

Consumer provides a simple `ConsumeOne` function that will fetch one message. 
It's up to you to build a consumption loop or goroutine pool to process messages concurrently.

Here is a simple consumption of 1 message:

```go
msg, err := c.ConsumeOne(context.Background(), "my-queue")
```

Then you'll process the message.
If processing is successful, you have to acknowledge the message, otherwise it will be re-delivered after the max processing time.

```go
err = c.Ack(context.Background(), "my-queue", msg)
```

If processing failed, you have to nack the message:

```go
err = c.Nack(context.Background(), "my-queue", msg)
```

`Ack` and `Nack` take the whole message (not just the ID) because the server requires the delivery receipt
from the consume response: it fences the ack/nack to that exact delivery, so a late ack/nack from a consumer
that exceeded the max processing time cannot affect a redelivery owned by another consumer. The SDK sends
the receipt for you.

## Java SDK

The Java SDK code is available at [GitHub](https://github.com/n0rdy/forq-sdk-java)

It is available in the [Maven Central Repository](https://central.sonatype.com/artifact/sh.forq/forqsdk)

```xml
<dependency>
    <groupId>sh.forq</groupId>
    <artifactId>forqsdk</artifactId>
    <version>${forq-version}</version>
</dependency>
```

where `${forq-version}` is the latest version, e.g. `0.1.0`

### Producer

```java
var producer = new ForqProducer(httpClient, "http://localhost:8080", "your-auth-secret-min-32-chars-long");
```

where `httpClient` is an instance of Apache HttpClient 5 `CloseableHttpClient` that you have to initialize with necessary timeouts, connection pool sizes, etc.

You might ask why not use `java.net.http.HttpClient` that is part of the JDK? The native Java HTTP Client has a [bug with GOAWAY frames](https://bugs.openjdk.org/browse/JDK-8335181) that was fixed only in Java 24, which is a too hard ask.

Please note that the classic (blocking) Apache HttpClient API speaks HTTP/1.1: each in-flight long poll occupies one pooled connection.
If you run many concurrent consumers from one JVM, raise the connection pool limits accordingly
(`PoolingHttpClientConnectionManager` `setMaxTotal`/`setDefaultMaxPerRoute` - the per-route default is only 5).

You can then use the producer to send messages:

```java
// processAfter is optional: pass a future Unix-ms timestamp for delayed delivery, or null for immediate
var newMessage = new NewMessageRequest("I am going on an adventure!", System.currentTimeMillis() + 3_600_000);

try {
    producer.sendMessage(newMessage, "my-queue");
} catch (IOException e) {
    // thrown by either Jackson while serializing the request, or by the HTTP client while sending the request
    // process it here
} catch (ErrorResponseException e) {
    // thrown if Forq server returned non-2xx response
    // process it here by fetching status code via `e.getHttpStatusCode()` and error response body via `e.getErrorResponse()`
}
```

### Consumer

```java
var consumer = new ForqConsumer(httpClient, "http://localhost:8080", "your-auth-secret-min-32-chars-long");
```

where `httpClient` is an instance of Apache HttpClient 5 `CloseableHttpClient`. The SDK sets the response timeout
for the consume call itself (long polling needs at least 40 seconds), so no special timeout tuning is needed.

You can then use the consumer to fetch messages:

```java
try {
    var msgOptional = consumer.consumeOne("my-queue");
} catch (IOException e) {
    // thrown by either Jackson while deserializing the response, or by the HTTP client while sending the request
    // process it here
} catch (ErrorResponseException e) {
    // thrown if Forq server returned non-2xx response
    // process it here by fetching status code via `e.getHttpStatusCode()` and error response body via `e.getErrorResponse()`
}
```

`msgOptional` is `Optional<MessageResponse>`, as according to the Forq API, if there is no message available, the response will be `204 No Content`.

Then you'll process the message. 
If processing is successful, you have to acknowledge the message, otherwise it will be re-delivered after the max processing time.
```java
try {
    consumer.ack("my-queue", msg);
} catch (IOException e) {
    // thrown by either Jackson while serializing the request, or by the HTTP client while sending the request
    // process it here
} catch (ErrorResponseException e) {
    // thrown if Forq server returned non-2xx response
    // process it here by fetching status code via `e.getHttpStatusCode()` and error    
    // response body via `e.getErrorResponse()`
}
```

If processing failed, you have to nack the message:
```java
try {
    consumer.nack("my-queue", msg);
} catch (IOException e) {
    // thrown by either Jackson while serializing the request, or by the HTTP client while sending the request
    // process it here
} catch (ErrorResponseException e) {
    // thrown if Forq server returned non-2xx response
    // process it here by fetching status code via `e.getHttpStatusCode()` and error
    // response body via `e.getErrorResponse()`
}
```

`ack` and `nack` take the whole `MessageResponse` (not just the ID) because the server requires the delivery
receipt from the consume response - the SDK sends it for you via the `X-Forq-Receipt` header.

## TypeScript SDK

The TypeScript SDK code is available at [GitHub](https://github.com/n0rdy/forq-sdk-typescript)

It is available in the [NPM registry](https://www.npmjs.com/package/@forq/sdk)

```bash
npm install @forq/sdk
```

### Producer

You can create a new producer by providing Forq server URL and auth secret:

```typescript
const producer = new ForqProducer(
    'https://your-forq-server.com',
    'your-auth-secret-min-32-chars-long'
);
```

You can then use the producer to send messages:

```typescript
const queueName = 'my-queue';
const newMessage: NewMessageRequest = {
    content: 'I am going on an adventure!',
    processAfter: Date.now() + 3_600_000, // optional: deliver in 1 hour; omit for immediate delivery
};

async function sendMessageWithErrorHandling() {
    try {
        await producer.sendMessage(newMessage, 'my-queue');
    } catch (error) {
        if (error instanceof ForqError) {
            console.error(`ForqError: Status ${error.httpStatusCode} and error response ${error.errorResponse}`, error);
        } else {
            console.error('Unexpected error:', error);
        }
    }
}
```

Or use `.then(...).catch(...)` if you prefer promises.

### Consumer

You can create a new consumer by providing Forq server URL and auth secret:

```typescript
const consumer = new ForqConsumer(
    'https://your-forq-server.com',
    'your-auth-secret-min-32-chars-long'
);
```

You can then use the consumer to fetch messages:

```typescript
async function consumeMessage(): Promise<MessageResponse | null> {
    try {
        const message: MessageResponse | null = await consumer.consumeOne('my-queue');
        
        if (message) {
            console.log('Message received:', message);
            console.log('Message ID:', message.id);
            console.log('Message content:', message.content);
            return message;
        } else {
            console.log('No messages available in queue');
            return null;
        }
    } catch (error) {
        if (error instanceof ForqError) {
            console.error(`ForqError during consume: Status ${error.httpStatusCode} and error response ${error.errorResponse}`, error);
        } else {
            console.error('Unexpected error during consume:', error);
        }
        throw error;
    }
}
```

Then you'll process the message.
If processing is successful, you have to acknowledge the message, otherwise it will be re-delivered after the max processing time.

```typescript
async function acknowledgeMessage(message: MessageResponse): Promise<void> {
    try {
        await consumer.ack('my-queue', message);
        console.log(`Message ${message.id} acknowledged successfully`);
    } catch (error) {
        if (error instanceof ForqError) {
            console.error(`ForqError during ack: Status ${error.httpStatusCode} and error response ${error.errorResponse}`, error);
        } else {
            console.error('Unexpected error during ack:', error);
        }
        throw error;
    }
}
```

If processing failed, you have to nack the message:

```typescript
async function nackMessage(message: MessageResponse): Promise<void> {
    try {
        await consumer.nack('my-queue', message);
        console.log(`Message ${message.id} nacked successfully`);
    } catch (error) {
        if (error instanceof ForqError) {
            console.error(`ForqError during nack: Status ${error.httpStatusCode} and error response ${error.errorResponse}`, error);
        } else {
            console.error('Unexpected error during nack:', error);
        }
        throw error;
    }
}
```

`ack` and `nack` take the whole `MessageResponse` (not just the ID) because the server requires the delivery
receipt from the consume response - the SDK sends it for you via the `X-Forq-Receipt` header.