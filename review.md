# Security & performance review

Date: 2026-08-16. Scope: full Go codebase (excludes the Hugo `site/` and `docs/`),
security and performance, checked against the design in CONTEXT.md. Findings verified
against the actual code; several were confirmed empirically (the `errors.As` behavior,
`for range` over a closed channel).

**TL;DR:** No injection or XSS holes: SQL is fully parameterized, the UI uses
`html/template` throughout, and CSRF is properly defended (nosurf token + SameSite=Lax +
HttpOnly). The problems are operational robustness, not break-ins. The standout is a
cluster of high-severity bugs where the code silently does something other than what the
comments and CONTEXT.md say: server timeouts are ~8 hours instead of 45 seconds, every
API business error returns HTTP 500, produce has no request body size limit, and
graceful shutdown is dead code with no signal handler. The queue engine's core claim
path is correctly atomic, but late ack/nack from a timed-out consumer isn't fenced.

## High severity

### 1. Server timeouts are ~8.3 hours, not 40-45 seconds (milliseconds x time.Second)

`configs/app.go:43,67-69`:

```go
pollingDurationMs := 30 * 1000
...
Handle: time.Duration(pollingDurationMs+10) * time.Second, // comment says 40s
Write:  time.Duration(pollingDurationMs+15) * time.Second, // comment says 45s
Read:   time.Duration(pollingDurationMs+15) * time.Second, // comment says 45s
```

`pollingDurationMs` is milliseconds (30000) but multiplied by `time.Second`, so `Handle`
is 30010s (~8h20m) and Read/Write are 30015s. These feed `http.TimeoutHandler` and the
`http.Server` Read/Write timeouts for both servers (`main.go:94-98,110-114`). Only
`ReadHeaderTimeout` (10s) is sane, so a client that authenticates can hold a handler
goroutine and connection open for 8+ hours via a slow body or slow read - cheap
goroutine/connection exhaustion of a single-process service. Found independently by three
of the four reviewers.

**Fix:** `time.Duration(pollingDurationMs+10_000) * time.Millisecond` (same for the
others), or define the constants as `time.Duration` directly.

### 2. Every API business error is returned as HTTP 500 (`errors.As` never matches)

`api/router.go:170-177`:

```go
var fe *common.ForqError
if errors.As(err, &fe) {
    ar.sendJsonResponse(w, http.StatusMultiStatus, fe.Code)
```

The sentinels in `common/errors.go:17-22` are **value** types (`ForqError{...}`, with a
value receiver on `Error()`), but the target is `**ForqError`. `errors.As` matches only
concrete `*ForqError`, so it always returns false and every error falls through to 500
`{"code":"internal"}`. Nack of an unknown/reclaimed message, oversized content, bad
`processAfter`, and DLQ-only violations all return 500 - consumers can't tell their own
4xx mistakes from real server faults. And the matched branch is doubly wrong anyway:
`http.StatusMultiStatus` (207) isn't an error status, and it marshals the bare `fe.Code`
string instead of `common.ErrorResponse`.

**Fix:** `var fe common.ForqError; errors.As(err, &fe)`, map codes to 400/404/500, and
send `ErrorResponse{Code: fe.Code}`.

### 3. No request body size limit on produce - 256KB checked after decoding an unbounded body

`api/router.go:83-90` + `services/messages.go:37`:

```go
err := json.NewDecoder(req.Body).Decode(&newMessage)   // unbounded read
...
if len(newMessage.Content) > ms.appConfigs.MessageContentMaxSizeBytes {  // too late
```

There is no `http.MaxBytesReader` anywhere in the codebase (verified by grep). An
authenticated or buggy producer can POST a multi-GB body; combined with #1's 8-hour read
timeout, that's a straightforward memory/connection exhaustion vector, and the documented
256KB cap is only enforced after full allocation.

**Fix:** `req.Body = http.MaxBytesReader(w, req.Body, 256*1024 + overhead)` before
decoding, return 413 on `*http.MaxBytesError`.

### 4. Graceful shutdown is dead code, and there is no signal handling at all

`main.go:83-84,119-166`:

```go
shutdownCh := make(chan struct{})
...
shutdownOnce.Do(func() { close(shutdownCh) })   // only ever closed, never sent to
...
for range shutdownCh {                          // zero iterations over a closed channel
    ... apiServer.Shutdown(...)
```

`for range` over a channel that is closed without values sent runs its body zero times,
so `Shutdown` is never called even on the internal-failure path. And there is no
`signal.Notify`/`NotifyContext` anywhere (verified by grep), so on SIGTERM/SIGINT (every
`docker stop` / Coolify redeploy) the process is killed abruptly: `defer repo.Close()`
and job `Close()`s never run, in-flight receives drop, and every in-flight message stays
invisible for up to the 5-minute stale-recovery window after a routine redeploy.

**Fix:** `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`,
block on `<-ctx.Done()`, then run the shutdown sequence unconditionally: server
`Shutdown` with a deadline shorter than the 30s long-poll, then jobs, then repo. Also
thread a shutdown context into the long-poll loop so consumers return promptly instead of
blocking shutdown up to ~30s.

## Medium severity

### 5. No delivery fencing: late ack/nack from a timed-out consumer hits another consumer's message

`db/repo.go:614-623` (ack), `336-361` (nack), `379-401` (stale reclaim). Ack and nack
match only `id + queue + status = processing`; there's no per-delivery token or attempt
fence:

```sql
DELETE FROM messages WHERE id = ? AND queue = ? AND status = ?;  -- status = processing
```

Scenario: consumer A exceeds the 5-min visibility timeout; the stale job (`stale.go`,
3-min interval) resets the message to ready; consumer B claims it. Now A's late **ack**
deletes the row B is actively processing (B's later ack is a silent no-op), and A's late
**nack** flips B's in-flight delivery back to ready with backoff so consumer C also gets
it - two consumers processing concurrently with neither past its timeout, and `attempts`
corrupted. At-least-once tolerates redelivery, but the API shouldn't corrupt state
silently. Compounding it, ack-on-zero-rows returns success (`repo.go:635-637` only logs a
warning) and `AckMessage` still increments `forq_messages_acked_total`, so the counter
overstates acks.

**Fix:** return `attempts` (or `processing_started_at`) as a receipt on consume and
require it in ack/nack (`... AND attempts = ?`); return `ErrNotFoundMessage` on 0-row ack
as nack already does (`repo.go:373-375`); only count the ack when a row was deleted.

### 6. Auth-throttle lockout rejects valid credentials - DoS of all clients behind a shared IP

`api/middleware.go:34-38` checks `IsLocked(ip)` before the key check, so a locked IP is
429'd even with the correct key. With the default `FORQ_TRUST_PROXY_HEADERS=false`,
`ClientIP` returns `RemoteAddr`; behind the reverse proxy CONTEXT.md recommends, every
client resolves to the proxy's IP. An unauthenticated attacker sends 5 bogus-key requests
(→ 60s lockout), repeats every minute, and permanently 429s every producer, consumer,
metrics scrape, and UI login. The `main.go` proxy-spoof warning covers the spoofing
direction, not this shared-IP direction.

**Fix:** verify the key first (constant-time) and let a valid key bypass the lockout, so
the lockout only ever throttles actual failures. Document that behind a proxy you must set
`FORQ_TRUST_PROXY_HEADERS=true`.

### 7. Expired-message sweeps can't use any index and run as one unbounded write statement

`db/repo.go:461-487,665-675`:

```sql
WHERE status != ? AND is_dlq = FALSE AND expires_after < ?;   -- regular
WHERE status != ? AND is_dlq = TRUE  AND expires_after < ?;   -- DLQ
```

The only candidate index is `idx_expired (status, is_dlq, expires_after)`, but SQLite
can't use an index whose leading column is constrained by `!=`, so both sweeps full-scan,
every 5 min (regular) and 62 min (DLQ), on the single write connection - stalling all
produces/consumes/acks/nacks while they run. On a large expired backlog this can livelock:
the unbounded UPDATE blocks writes for up to ~5 min, the `interval-1s` context interrupts
it, it rolls back, and it repeats every tick without completing.

**Fix:** `status IN (0, 2)` (literals expand to two index range scans), and batch the
sweep (`... WHERE id IN (SELECT id ... LIMIT 1000)` looped until 0 rows, checking ctx
between batches). Same for the DLQ delete.

### 8. `forq_queue_depth` goes stale instead of dropping to zero; queue-name label cardinality is unbounded

`jobs/metrics/queuesdepth.go:27-34`, `db/repo.go:233-238`, `metrics/prometheus.go:140-142`.
Two problems: (a) the depth job derives queues from `GROUP BY queue, is_dlq` over live
rows, so a queue that drains to zero or is purged simply vanishes from the result set and
the job never resets it - Prometheus reports the last non-zero depth forever, so an alert
on `queue_depth > N` fires permanently for an empty queue. (b) Queue names are unvalidated
URL segments; anyone with the shared secret can mint unlimited names, and each becomes a
permanent label on five counter vecs plus the gauge (Prometheus never GCs counters),
growing forq's and Prometheus's memory without bound.

**Fix:** `queueDepth.Reset()` before repopulating each cycle (or delete vanished series),
and validate queue names at the boundary (see #9).

### 9. No queue-name validation anywhere - state confusion and misdirected destructive UI actions

`chi.URLParam(req, "queue")` flows to SQL and templates with no length/charset check
(`api/router.go:92,103,118`). Parameterized queries stop injection, but:

- A producer can POST directly into `foo-dlq`; the insert never sets `is_dlq`
  (`db/repo.go:120-140`), so the dashboard (trusts the column) calls it "Regular" while
  the queue page (trusts the suffix) calls it a DLQ, "Requeue all" moves it into `foo`,
  and a 5x failure re-appends the suffix to make `foo-dlq-dlq`.
- Names with `#` or `?` misdirect destructive UI actions: a name `orders-dlq#x`
  interpolated into `hx-delete="/queue/{{.Name}}/messages"` (`queue.html:57`, HTML- but
  not URL-escaped) resolves in the browser to `/queue/orders-dlq` - "Delete All" purges a
  *different* queue than the one the admin confirmed.

**Fix:** validate at the API boundary (e.g. `^[a-zA-Z0-9._-]{1,64}$`), reject the `-dlq`
suffix on produce, validate `messageId` as a UUID, and URL-escape names when building
template URLs.

### 10. Empty long polls hammer the single write connection

`services/messages.go:83-115` is a 500ms busy-poll, and each probe is the claiming
`UPDATE ... RETURNING` on `dbWrite` (`SetMaxOpenConns(1)`). N idle consumers = 2N
write-connection acquisitions/sec serialized on the one connection the 1-5k msg/s target
depends on, un-jittered so consumers herd, competing with the #7 sweeps.

**Fix:** probe with a cheap `SELECT EXISTS` on the read pool and only run the UPDATE when
a candidate exists; better, add an in-process per-queue notify channel signaled on
insert/nack/requeue so idle consumers block instead of polling.

### 11. Timing-unsafe comparison of all three secrets

`api/middleware.go:41` (`authHeader != authSecret`, used for both the API and metrics
secrets) and `ui/router.go:88` (`token != ur.authSecret`). Go's `!=` short-circuits on
length then byte-by-byte, leaking prefix-match timing on the single token that is the
entire security model. Exploitation over a network is hard and the per-IP throttle helps,
but the fix is one line each.

**Fix:** `subtle.ConstantTimeCompare` in all three places.

### 12. Client disconnect during long poll logged as error and answered with 500

`services/messages.go:108-113` treats `<-ctx.Done()` as `log.Error(...)` + return
`ErrInternal`. Every consumer that hangs up early - normal for long polling - produces an
error-level log line and a doomed 500 write. Return `nil, nil` (or a no-response sentinel)
when `ctx.Err() == context.Canceled`.

## Low severity

- **Retry backoff off-by-one** (`db/repo.go:752-764`): the claim already did
  `attempts = attempts + 1`, so the `WHEN attempts + 1 = 1` arm (the 1s delay) is
  unreachable; actual delays are 5s, 15s, 30s, 60s, 60s vs the documented 1s, 5s, 15s,
  30s, 60s. Generate `WHEN attempts = %d`.
- **No `PRAGMA busy_timeout`** (`db/repo.go:36-52`): WAL + single write connection makes
  SQLITE_BUSY unlikely, but checkpointing and the leaked migrate connection (below) can
  still surface it as a hard error. Add `PRAGMA busy_timeout = 5000` to both pools.
- **`DbOptimizationMs` is 1 minute, comment says 1 hour** (`configs/app.go:62`):
  `1 * 60 * 1000` runs `PRAGMA optimize` 60x too often. Should be `60 * 60 * 1000`.
- **Migration engine leaks its connection and a second SQLite driver** (`main.go:299-324`):
  `m` is never closed, so the modernc-driver `sqlite://...cache=shared` connection stays
  open for the process lifetime alongside mattn/go-sqlite3. Call `m.Close()`.
- **CSP uses `'unsafe-inline'` + unpinned CDN scripts with no SRI** (`ui/middleware.go:20`,
  `ui/templates/base.html:9-13`): `daisyui@5` / `@tailwindcss/browser@4` float on major
  with no `integrity=`, so a bad release runs in the authenticated admin session; and
  `'unsafe-inline'` means CSP wouldn't catch an injection. Today template escaping is the
  only defense (it holds). Vendor the three assets into the existing embed FS, or pin
  exact versions + SRI and move the theme snippet to a served file to drop
  `'unsafe-inline'`.
- **Message browsing has no supporting index** (`db/repo.go:288-310`): `WHERE queue = ?
  ... ORDER BY id DESC LIMIT ?` has no `(queue, id)` index, so each infinite-scroll page
  sorts/scans a large slice at the design's 1M-message scale. Add `idx_queue_id ON
  messages(queue, id)`. (Pagination is otherwise done right: keyset not offset,
  server-fixed LIMIT 50, limit+1 look-ahead, bound cursor.)
- **Queue-depth job full-scans `idx_for_queue_depth` every 30s** (`db/repo.go:233-238`):
  on the read pool so it doesn't block writes, fine at target scale, a steady tax on a
  multi-million-row backlog.
- **Jobs have no panic recovery** (`jobs/cleanup/expired.go:22-38`, all six jobs): a
  panic in a tick takes down the whole process (and with #4 there's no clean teardown).
  Wrap each tick body in `defer func(){ recover() }()`.
- **Throttle cap eviction can evict locked entries** (`services/throttling.go:111-123`):
  unlike the sweep, `evictOldestEntryLocked` ignores `lockedUntil`, so ~10K fresh
  failures can flush an attacker's own 60s lockout early. Skip locked entries when
  evicting.
- **Authenticated UI pages are cacheable** (`ui/middleware.go:17-45`): no `Cache-Control`,
  so message content/failure reasons can sit in the browser disk cache and be viewed via
  back/forward after logout on a shared machine. Add `Cache-Control: no-store`.
- **Write-side health ping ignores context** (`db/repo.go:723`): `dbWrite.Ping()` not
  `PingContext`, so `/healthcheck` can hang on a wedged write connection.
- **Failed UI login returns HTTP 200** (`ui/router.go:88-96`): makes failed logins
  indistinguishable from successes to status-code-watching log tooling; the API path
  correctly returns 401.
- **Doc drift**: CONTEXT.md says stale detection every 2 min (code: 3 min), and
  `DELETE /queue/{queue}/messages` "any queue allowed" (code: DLQ-only, the safer
  behavior). `WITHOUT ROWID` with 256KB payloads is also against SQLite's own guidance
  (rows should be < ~1/20 page); measure or switch to a plain rowid table.
- **Avatar slice splits a byte, not a rune** (`dashboard.html:73`,
  `{{index (slice .Name 0 1)}}`): a name starting with a multi-byte rune renders as
  U+FFFD.

## What's in good shape

- **The receive path is genuinely atomic**: `UPDATE ... WHERE id = (SELECT ... LIMIT 1)
  RETURNING` on a single serialized write connection (`db/repo.go:147-173`) - no two
  consumers can claim the same message, riding the partial covering index
  `idx_queue_ready_for_consuming`. Failed-to-DLQ and expired-to-DLQ moves are single
  atomic UPDATEs; the same-table `-dlq` suffix avoids cross-table two-phase moves.
- **No XSS**: `html/template` used consistently (`ui/templates.go`), every render of
  producer-written content, failure reasons, and queue names is contextually escaped, and
  there is no `template.HTML` / `text/template` / Sprintf-built HTML anywhere in `ui/`.
- **Real CSRF defense**: nosurf (post-CVE-2025-46721 version) wraps the whole UI router,
  every mutating element sends `X-CSRF-Token` via `hx-headers`, and cookies are
  SameSite=Lax + HttpOnly (+ Secure in pro). A cross-site purge can't fire - it lacks both
  the token and the cookie.
- **Session hygiene**: crypto-random UUIDv4 IDs, fresh ID per login (no fixation), expiry
  enforced on every check + hourly sweep, logout invalidates server-side and clears the
  cookie.
- **State-machine guards live in the SQL**: nack requires `status = processing`, DLQ
  requeue/expiry exclude `processing`, stale recovery and nack share the attempts-vs-max
  CASE, so API and jobs can't double-transition a row. Nack correctly returns
  `ErrNotFoundMessage` on 0 rows (ack should copy this - see #5).
- **All SQL parameterized**; the one `fmt.Sprintf` (backoff CASE) interpolates only config
  integers. Job counters use `RowsAffected()` so they reflect real row counts.
- **Config/secrets**: fail-fast 32-char minimum on both secrets, metrics behind a separate
  secret, env-var-only config, localhost-default binds, sensible security headers with
  HSTS in pro, correct rightmost-XFF handling with an opt-in trust flag and startup
  warning. Job loops are single-goroutine tickers (no self-overlap) with per-run context
  deadlines. Read/write pool split with a `hard_heap_limit` OOM guard is a nice touch.

## Suggested fix order

1. Timeout units (#1) - one-line diff, removes an 8-hour DoS window
2. Signal handling + live shutdown loop (#4) - clean redeploys
3. `errors.As` value-vs-pointer + status mapping (#2) - API correctness
4. `MaxBytesReader` on produce (#3)
5. Expired-sweep `status IN` + batching (#7), then delivery fencing (#5)
6. Queue-name validation (#9), which also bounds metric cardinality (#8)
