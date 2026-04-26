---
title: "Configurations"
slug: "configurations"
description: "All configuration options for Forq"
lead: "Detailed list of all configuration options available in Forq."
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "configurations"
weight: 103
toc: true
---

Based on Forq philosophy of being simple yet opinionated, most of the configuration options are sensible defaults and can't be changed.
You can see them in the [source code](https://github.com/n0rdy/forq/blob/main/configs/app.go).

However, there are a few options that can be configured via environment variables. Let me walk you through them.

# Configuration Options

## TLDR

```bash
# Required
export FORQ_AUTH_SECRET=your-auth-secret-min-32-chars-long                # to use for API and Admin UI authentication
export FORQ_DB_PATH=./data/forq.db                                        # path to the SQLite database file

# Optional
export FORQ_METRICS_ENABLED=false                                         # true|false (default: false)
export FORQ_METRICS_AUTH_SECRET=your-metrics-secret-min-32-chars-long     # required if FORQ_METRICS_ENABLED is true
export FORQ_ENV=pro                                                       # local|pro (default: pro)
export FORQ_QUEUE_TTL_HOURS=24                                            # Default: 24 hours
export FORQ_DLQ_TTL_HOURS=168                                             # Default: 168 hours (7 days)
export FORQ_API_ADDR=localhost:8080                                       # Default: localhost:8080
export FORQ_UI_ADDR=localhost:8081                                        # Default: localhost:8081
export FORQ_TRUST_PROXY_HEADERS=false                                     # true|false (default: false) - only enable behind a trusted proxy that strips/replaces client X-Forwarded-For
```

## Detailed Explanation

### Auth Secret (FORQ_AUTH_SECRET)

This is the only required configuration. It is used to secure the API and Admin UI.

- **Type**: String
- **Default**: None (must be set)
- **Required**: Yes
- **Requirement**: Must be at least 32 characters long

```bash
export FORQ_AUTH_SECRET=your-auth-secret-min-32-chars-long
```

#### Recommendations: 

- use a strong, randomly generated string. You can use tools like `openssl`, `pwgen` or your password manager to generate a secure secret.
- apply Gandalf's rule of thumb to this value: "Keep it secret, keep it safe".

#### Usage:

- while making calls to the API, you will need to provide this secret in the `X-API-Key` header.
- while accessing the Admin UI, you will be prompted to enter this secret.

### Database Path (FORQ_DB_PATH)

Set the path to the SQLite database file used by Forq to store messages and metadata.
Make sure that the path is writable and consistent across restarts.
For example, if you are using a relative path, make sure you always start Forq from the same working directory.

- **Type**: String
- **Default**: None (must be set)
- **Required**: Yes

```bash
export FORQ_DB_PATH=./data/forq.db
```

#### Recommendations:
- if you are running Forq in a containerized environment, consider using a volume mount to persist the database file outside the container
- ensure that the database file is not accessible from the web for security reasons
- make sure that the directory where the database file is located exists and is writable by the user running Forq
- if you are using a relative path, ensure that you always start Forq from the same working directory to avoid losing access to the database file, as DB will be recreated in the new working directory and empty

### Metrics Enabled (FORQ_METRICS_ENABLED)

Enable or disable Prometheus metrics endpoint. Metrics are disabled by default.

- **Type**: Boolean
- **Default**: false
- **Required**: No

```bash
export FORQ_METRICS_ENABLED=false  # or true
```

Find more about metrics in the [Metrics Guide](/documentation-portal/docs/guides/metrics/).

### Metrics Auth Secret (FORQ_METRICS_AUTH_SECRET)

This secret is used to secure the metrics endpoint. It is required if `FORQ_METRICS_ENABLED` is set to `true`.

- **Type**: String
- **Default**: None
- **Required**: If `FORQ_METRICS_ENABLED` is `true`
- **Requirement**: Must be at least 32 characters long

```bash
export FORQ_METRICS_AUTH_SECRET=your-metrics-secret-min-32-chars-long
```

#### Recommendations:

- use a strong, randomly generated string. You can use tools like `openssl`, `pwgen` or your password manager to generate a secure secret.
- apply Gandalf's rule of thumb to this value: "Keep it secret, keep it safe".

#### Usage:

- while scraping the metrics endpoint, you will need to provide this secret in the `X-API-Key` header.

### Environment (FORQ_ENV)

Set the environment in which Forq is running. Either `local` or `pro`.

- **Type**: String
- **Default**: pro
- **Required**: No
- **Requirement**: Must be either `local` or `pro`

```bash
export FORQ_ENV=pro  # or local
```

#### Usage:

- setting this to `local` will run the server in HTTP mode, which is useful for local development

### Queue TTL Hours (FORQ_QUEUE_TTL_HOURS)

Set the time-to-live (TTL) for messages in the main queue. After this duration, unacknowledged messages will be moved to the dead-letter queue (DLQ).

If the message has been sent with the delay option via `processAfter` param, the TTL countdown starts after the process after time has passed and the message is visible for consumption.

- **Type**: Integer
- **Default**: 24 (hours)
- **Required**: No

```bash
export FORQ_QUEUE_TTL_HOURS=24  # in hours
```

#### Recommendations:

- set this value based on your application's requirements for message processing time
- remember that the longer the TTL, the more disk memory Forq will use to store unacknowledged messages

### Dead-Letter Queue TTL Hours (FORQ_DLQ_TTL_HOURS)

Set the time-to-live (TTL) for messages in the dead-letter queue (DLQ). After this duration, messages in the DLQ will be permanently deleted.

- **Type**: Integer
- **Default**: 168 (hours) (7 days)
- **Required**: No

```bash
export FORQ_DLQ_TTL_HOURS=168  # in hours
```

#### Recommendations:
- set this value based on your application's requirements for how long you want to retain failed messages for inspection or reprocessing
- remember that the longer the TTL, the more disk memory Forq will use to store messages
- usually, this value should be significantly longer than `FORQ_QUEUE_TTL_HOURS`, so you have enough time to inspect and handle failed messages
- but it depends on your use case, use your judgment

### API Address (FORQ_API_ADDR)

Set the address and port on which the Forq API server will listen.

- **Type**: String
- **Default**: localhost:8080
- **Required**: No

```bash
export FORQ_API_ADDR=localhost:8080
```

#### Usage:
- set this value based on your deployment environment and network configuration: some might want to allow remote access to the API, while others might want to restrict it to localhost only due to the use of reverse proxies or SSH tunnels
- make sure that the API address is different from the UI address to avoid port conflicts
- API and UI can use the same host, just different ports

### UI Address (FORQ_UI_ADDR)

Set the address and port on which the Forq Admin UI server will listen.

- **Type**: String
- **Default**: localhost:8081
- **Required**: No

```bash
export FORQ_UI_ADDR=localhost:8081
```

#### Usage:
- set this value based on your deployment environment and network configuration: some might want to allow remote access to the Admin UI, while others might want to restrict it to localhost only due to the use of reverse proxies or SSH tunnels
- ensure that this port is accessible from your browser if you're accessing the Admin UI remotely
- make sure that the UI address is different from the API address to avoid port conflicts
- API and UI can use the same host, just different ports

### Trust Proxy Headers (FORQ_TRUST_PROXY_HEADERS)

Controls how Forq determines the client IP for login throttling and API key throttling.

- **Type**: Boolean
- **Default**: false
- **Required**: No

```bash
export FORQ_TRUST_PROXY_HEADERS=false  # or true
```

#### Behavior:

- **`false` (default)**: Forq uses the direct connection's source address (`RemoteAddr`) as the client IP. Correct when Forq is exposed directly to clients.
- **`true`**: Forq reads the rightmost entry of `X-Forwarded-For` as the client IP, falling back to `RemoteAddr` if the header is absent or malformed. Required when Forq runs behind a reverse proxy, otherwise every request appears to come from the proxy's IP and one bad client can lock everyone out.

#### When to enable:

- You run Forq behind nginx, Caddy, Traefik, HAProxy, a cloud load balancer, or any other reverse proxy that adds `X-Forwarded-For`.

#### Security warning:

- **Only enable this when Forq is actually behind a trusted reverse proxy.** If Forq is reachable directly without a proxy in front, attackers can spoof the `X-Forwarded-For` header to make their requests appear to come from any IP, bypassing throttling entirely.
- Your proxy must strip or replace any incoming `X-Forwarded-For` header from clients before forwarding. nginx, Caddy, Traefik, and most cloud load balancers do this correctly by default, but verify your configuration if in doubt.
- This setting assumes a single proxy hop in front of Forq. Multi-proxy chains (CDN, then load balancer, then Forq) should be canonicalized at the edge: have your innermost proxy overwrite `X-Forwarded-For` with the real client IP before forwarding to Forq, regardless of what was forwarded earlier in the chain.
