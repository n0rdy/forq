# Forq Benchmarks

Two benchmark suites live here:

1. **HTTP API harness** (`main.go`) - load-tests the real Forq application over
   its HTTP API: produce, consume (long polling), ack with delivery receipts.
2. **Schema micro-benchmark** (`schema/`) - SQLite-level comparison of table
   layout choices using Forq's exact DDL, indexes, pragmas, and hot-path
   queries.

## HTTP API Harness

### What it measures

- **produce / consume / ack RTTs, separately.** The consume timer covers only
  polls that returned a message - a long poll sitting idle waits for a
  *producer*, and counting that as latency would measure producer cadence, not
  Forq.
- **End-to-end delivery latency** (produce timestamp → consume), recorded from
  a timestamp embedded in each payload. This is the queue metric users
  actually feel. With a backlog it is dominated by queue depth (FIFO: new
  messages wait behind the backlog) - honest queue behavior, not overhead.
- **Duplicate deliveries**, detected via message IDs across all consumers. A
  correctness signal: expected to be 0 unless consumers exceed the 5-minute
  visibility timeout.
- **Throughput** (produced/sec, consumed/sec) over the measurement window,
  with a warmup period excluded from all numbers.

### Self-contained by default

With no flags, the harness builds the parent module, launches a Forq server on
free ports with a temp database, waits for the healthcheck, runs the load, and
tears everything down:

```bash
go run . -scenario 10c10p -duration 30s
```

To benchmark an externally managed server instead:

```bash
go run . -api http://localhost:8080 -auth your-auth-secret-min-32-chars-long -scenario 10c10p
```

### HTTP/2

Forq's docs recommend HTTP/2 (h2c) for long-polling consumers. The default Go
HTTP client speaks HTTP/1.1, so the harness has an explicit switch:

```bash
go run . -scenario 40c20p -duration 30s -http2
```

Compare both modes to see what the HTTP/1.1 connection-per-poll model costs at
your consumer count.

### Flags

| Flag        | Default      | Meaning                                                        |
|-------------|--------------|----------------------------------------------------------------|
| `-scenario` | `1c1p`       | `1c1p`, `10c10p`, `40c20p`, `20c40p` (consumers/producers)     |
| `-duration` | `2m`         | measurement window (excludes warmup)                           |
| `-warmup`   | `10s`        | warmup before measurement starts                               |
| `-backlog`  | `1000`       | messages pre-seeded into the queue                             |
| `-size`     | `1024`       | payload size in bytes                                          |
| `-rate`     | `0`          | per-producer msgs/sec; `0` = unthrottled (saturation)          |
| `-http2`    | `false`      | use h2c instead of HTTP/1.1                                    |
| `-api`      | *(managed)*  | external Forq API base URL; empty = build & manage a server    |
| `-auth`     |              | auth secret (required with `-api`)                             |
| `-forq-bin` | *(go build)* | prebuilt forq binary for the managed server                    |
| `-json`     |              | write results JSON to a path (`-` = stdout) for tracking runs  |

Add new scenarios by extending the `scenarios` map in `main.go`.

## Schema Micro-Benchmark (`schema/`)

SQLite-level benchmark comparing the messages table as a plain rowid table vs
`WITHOUT ROWID`, using Forq's exact DDL, indexes, pragmas, and hot-path
queries (insert → claim → ack) at 1KB/10KB/50KB payloads.

Context: SQLite's guidance says `WITHOUT ROWID` tables want rows under ~1/20th
of a page, and Forq payloads are far larger. Measured result: the plain rowid
table is 15-30% faster on the hot path, which is why Forq uses one.

```bash
go test -bench=. -benchtime=2s ./schema/
```
