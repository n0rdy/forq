---
title: "Metrics"
slug: "metrics"
description: "Setting up monitoring with Prometheus for Forq"
lead: "Learn how to enable and configure metrics collection in Forq using Prometheus."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "metrics"
weight: 106
toc: true
---

Forq relies on [Prometheus](https://prometheus.io/) for metrics collection and monitoring.
Let me show you how to enable and configure it if you need it.

## Why you might need Metrics

Since Forq is a self-hosted service, it might be useful to see how it's performing and get alerted if something goes wrong.

Metrics can help to answer questions like:
- How many messages are being processed per second?
- Are there any messages getting stuck in the queue?
- How many messages are being moved to the dead-letter queue (DLQ)?
- Are there any consumers that are not ack-ing or nack-ing messages?
- How many stale messages are being recovered?
- What is the current depth of the queue?

If you are using tools like Grafana, you can easily create dashboards and set up alerts based on these metrics, 
so then you are awaken in the middle of the night if something goes wrong. Sounds appealing, right? =)

## Enabling Metrics

Metrics are disabled by default, as I believe they are more nice-to-have for the target audience of Forq rather than a
must-have.
It's super-duper easy to enable them, though by setting a couple of environment variables:

```bash
export FORQ_METRICS_ENABLED=true
export FORQ_METRICS_AUTH_SECRET=your-metrics-secret-min-32-chars-long
```

`FORQ_METRICS_ENABLED` enables the metrics endpoint, while `FORQ_METRICS_AUTH_SECRET` sets a secret that will be
required to access the metrics.
The secret must be at least 32 characters long.

Do remember Gandalf's rule of thumb about secrets: "Keep it secret, keep it safe."

## Polling Metrics

Once enabled, Forq will expose a `/metrics` endpoint on the same address as the API (default: `localhost:8080`).
You can poll it with `curl` or any HTTP client:

```bash
curl -H "X-API-Key: your-metrics-secret-min-32-chars-long" http://localhost:8080/metrics
```

The endpoint is fully managed by Prometheus, so if you are using smth like Grafana, it knows how to scrape it.

## Available Metrics

Forq exposes the following metrics:

| Metric Name                           | Description                                                                      | Type    |
|---------------------------------------|----------------------------------------------------------------------------------|---------|
| `forq_messages_produced_total`        | Total number of messages submitted to Forq by producers                          | Counter |
| `forq_messages_consumed_total`        | Total number of messages consumed by consumers.                                  | Counter |
| `forq_messages_acked_total`           | Total number of messages acknowledged by Forq                                    | Counter |
| `forq_messages_nacked_total`          | Total number of messages nacknowledged by Forq                                   | Counter |
| `forq_messages_requeued_total`        | Total number of messages moved from DLQ back to main queue manually by the admin | Counter |
| `forq_queue_depth`                    | Current depth of the queue                                                       | Gauge   |
| `forq_messages_moved_to_dlq_total`    | Total number of messages moved to dead-letter queue                              | Counter |
| `forq_messages_stale_recovered_total` | Total number of stale messages recovered                                         | Counter |
| `forq_messages_cleanup_total`         | Total number of messages cleaned up from DLQs                                    | Counter |

Additionally, Prometheus can scrape Go runtime metrics, such as memory usage and garbage collection stats.
I'm not listing them here, as they are subject to change and not Forq-specific.
Prometheus Go client [go_collector.go file](https://github.com/prometheus/client_golang/blob/main/prometheus/go_collector.go) can be a good place to look for them.

Let's discuss each metric in detail.

### forq_messages_produced_total

This counter increments every time a message is successfully submitted to Forq by a producer.
"Successfully submitted" means that the message has been validated and stored in the database.

#### Labels

- `queue_name`: the name of the queue the message was submitted to
- `queue_type`: either `regular` or `dlq`, depending on whether the message was submitted to a regular queue or a dead-letter queue

You might ask why do we have a `queue_type` label here if this counter comes from the producer flow. 
Well, in Forq, DQLs are just as regular queues, so, theoretically, nothing stops you from submitting messages directly to a DLQ.
Not sure why you'd want to do that, but we can still be friends =)

### forq_messages_consumed_total

This counter increments every time a message is successfully sent to a consumer.
"Successfully sent" means that the message has been fetched from the database and returned in the API response.

Please, note, this doesn't mean ack-ed or nack-ed, just fetched for processing.

#### Labels

- `queue_name`: the name of the queue the message was consumed from
- `queue_type`: either `regular` or `dlq`

Same as with producing messages, nothing stops you from consuming messages from a DLQ directly.

### forq_messages_acked_total

This counter increments every time a message is successfully acknowledged by a consumer.

#### Labels

- `queue_name`: the name of the queue the message was ack-ed from
- `queue_type`: either `regular` or `dlq`

### forq_messages_nacked_total

This counter increments every time a message is nacknowledged by a consumer.

#### Labels

- `queue_name`: the name of the queue the message was nack-ed from
- `queue_type`: either `regular` or `dlq`

### forq_messages_requeued_total

This counter increments every time a message is moved from a DLQ back to the main queue manually by the admin via the Admin UI.

As [Admin UI guide](/docs/guides/admin-ui/) explains, this is a manual operation that requires admin privileges, 
where the admin can requeue either 1 message or all messages from a DLQ back to the main queue.

#### Labels

- `queue_name`: the name of the queue the message was requeued from

There is no `queue_type` label here, it's only possible to requeue messages from a DLQ.

### forq_queue_depth

This gauge shows the current depth of the queue, i.e. how many messages are currently in the queue waiting to be consumed.
It covers all the existing queues at the moment of setting the gauge.

#### Labels

- `queue_name`: the name of the queue
- `queue_type`: either `regular` or `dlq`

### forq_messages_moved_to_dlq_total

This counter increments every time a message is moved to a DLQ once it became failed, 
i.e. it was nack-ed / stale more than `max_retries` times, or its TTL expired.

#### Labels

- `reason`: the reason why the message was moved to DLQ, either `failed` or `expired`

There is no `queue_name` label here, even though I do agree it would be useful.
However, this is an implementation trade-off: the moving op is performed by the cronjob with a simple query `UPDATE ... WHERE ...`,
which doesn't segregate by queue name. 
To get the queue name for each affected message, I'd need to have to `GROUP BY` queue name instead of doing fire-and-forget `UPDATE`.
The performance impact of that would be too high, so I decided to skip the label. Opinionated, remember?

There is no `queue_type` label here, as this metric shows when the message is moved from the Regular queue to DQL.

### forq_messages_stale_recovered_total

This counter increments every time a stale message is recovered by the dedicated cronjob.
A message is considered stale if it was fetched by a consumer but not ack-ed or nack-ed within the max processing time.

#### Labels

No labels. The explanation is the same as for `forq_messages_moved_to_dlq_total`: I prioritized the query performance over the label usefulness.

### forq_messages_cleanup_total

This counter increments every time a message is permanently deleted from a DLQ.
It is possible in 3 scenarios:
- the message was failed to be processed above `max_retries` times (5) if you have consumers for that DLQ
- the message's TTL expired (default: 7 days)
- the message was deleted manually by the admin via the Admin UI

#### Labels

- `reason`: the reason why the message was cleaned up, either `failed`, `expired`, or `deleted_by_user`

There is no `queue_name` label here, as explained above.
There is no `queue_type` label here, as this metric shows when the message is deleted from a DLQ.
