// Forq HTTP API benchmark harness.
//
// Measures the real Forq application over its HTTP API - produce, consume,
// ack - with either a self-managed server (built and launched automatically,
// the default) or an externally started one (-api).
//
// What it measures, and why:
//   - produce / consume / ack RTTs are recorded separately, and the consume
//     timer only covers polls that returned a message: a long poll that sits
//     waiting for a producer measures producer cadence, not Forq.
//   - end-to-end delivery latency (produce timestamp -> consume) is recorded
//     from a timestamp embedded in each payload; this is the queue metric
//     that actually matters to users.
//   - duplicate deliveries are detected via the message IDs seen by all
//     consumers - a correctness signal, expected to be 0 unless consumers
//     exceed the visibility timeout.
//   - -http2 switches the client to h2c, the deployment mode Forq's docs
//     recommend; without it the benchmark exercises the HTTP/1.1 path.
//
// Example (fully self-contained, builds ../ and runs a 30s scenario):
//
//	go run . -scenario 10c10p -duration 30s -http2
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/http2"
)

const receiptHeader = "X-Forq-Receipt"

type config struct {
	Scenario  string        `json:"scenario"`
	Duration  time.Duration `json:"-"`
	Warmup    time.Duration `json:"-"`
	Backlog   int           `json:"backlog"`
	Size      int           `json:"messageSizeBytes"`
	Rate      float64       `json:"perProducerRatePerSec"` // 0 = unthrottled
	UseHTTP2  bool          `json:"http2"`
	QueueName string        `json:"queue"`
	APIURL    string        `json:"apiUrl"`
	Auth      string        `json:"-"`
}

type scenarioConfig struct {
	Consumers int
	Producers int
}

var scenarios = map[string]scenarioConfig{
	"1c1p":   {Consumers: 1, Producers: 1},
	"10c10p": {Consumers: 10, Producers: 10},
	"40c20p": {Consumers: 40, Producers: 20},
	"20c40p": {Consumers: 20, Producers: 40},
}

func main() {
	var (
		scenario = flag.String("scenario", "1c1p", "scenario: 1c1p, 10c10p, 40c20p, 20c40p")
		duration = flag.Duration("duration", 2*time.Minute, "measurement duration (excluding warmup)")
		warmup   = flag.Duration("warmup", 10*time.Second, "warmup before measurement starts")
		backlog  = flag.Int("backlog", 1000, "messages pre-seeded into the queue")
		size     = flag.Int("size", 1024, "message payload size in bytes")
		rate     = flag.Float64("rate", 0, "per-producer produce rate in msgs/sec; 0 = unthrottled (saturation)")
		useHTTP2 = flag.Bool("http2", false, "use h2c (HTTP/2 cleartext) - the recommended Forq deployment mode")
		apiURL   = flag.String("api", "", "external Forq API base URL; empty = build and manage a server automatically")
		auth     = flag.String("auth", "", "auth secret (required with -api)")
		forqBin  = flag.String("forq-bin", "", "path to a prebuilt forq binary for the managed server; empty = 'go build' the parent module")
		jsonOut  = flag.String("json", "", "write machine-readable results JSON to this path ('-' = stdout)")
	)
	flag.Parse()

	if _, ok := scenarios[*scenario]; !ok {
		log.Fatalf("unknown scenario %q", *scenario)
	}

	cfg := &config{
		Scenario:  *scenario,
		Duration:  *duration,
		Warmup:    *warmup,
		Backlog:   *backlog,
		Size:      *size,
		Rate:      *rate,
		UseHTTP2:  *useHTTP2,
		QueueName: "benchmark_queue",
		APIURL:    *apiURL,
		Auth:      *auth,
	}

	// self-managed server unless -api is given
	if cfg.APIURL == "" {
		srv, err := startManagedServer(*forqBin)
		if err != nil {
			log.Fatalf("failed to start managed forq server: %v", err)
		}
		defer srv.stop()
		cfg.APIURL = srv.apiURL
		cfg.Auth = srv.authSecret
	} else if cfg.Auth == "" {
		log.Fatal("-auth is required when using an external server via -api")
	}

	runner := newRunner(cfg)

	if err := runner.healthcheck(); err != nil {
		log.Fatalf("forq is not healthy: %v", err)
	}

	fmt.Printf("🚀 Forq HTTP API benchmark\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("API URL:   %s (%s)\n", cfg.APIURL, protocolName(cfg.UseHTTP2))
	fmt.Printf("Scenario:  %s (%d consumers, %d producers)\n", cfg.Scenario, scenarios[cfg.Scenario].Consumers, scenarios[cfg.Scenario].Producers)
	fmt.Printf("Warmup:    %v, duration: %v\n", cfg.Warmup, cfg.Duration)
	fmt.Printf("Backlog:   %d messages of %d bytes\n", cfg.Backlog, cfg.Size)
	if cfg.Rate > 0 {
		fmt.Printf("Rate:      %.1f msgs/sec per producer\n", cfg.Rate)
	} else {
		fmt.Printf("Rate:      unthrottled (saturation)\n")
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	if err := runner.seedBacklog(); err != nil {
		log.Fatalf("failed to seed backlog: %v", err)
	}

	results := runner.run()
	printResults(results)
	runner.printErrors()

	if *jsonOut != "" {
		writeJSON(*jsonOut, results)
	}
}

func protocolName(h2 bool) string {
	if h2 {
		return "HTTP/2 h2c"
	}
	return "HTTP/1.1"
}

// ---------------------------------------------------------------------------
// managed server
// ---------------------------------------------------------------------------

type managedServer struct {
	cmd        *exec.Cmd
	apiURL     string
	authSecret string
	workDir    string
}

func startManagedServer(binPath string) (*managedServer, error) {
	workDir, err := os.MkdirTemp("", "forq-benchmark-*")
	if err != nil {
		return nil, err
	}

	if binPath == "" {
		binPath = filepath.Join(workDir, "forq")
		fmt.Println("🔨 building forq from the parent module...")
		build := exec.Command("go", "build", "-o", binPath, ".")
		build.Dir = ".."
		if out, err := build.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("go build failed: %v\n%s", err, out)
		}
	}

	apiPort, err := freePort()
	if err != nil {
		return nil, err
	}
	uiPort, err := freePort()
	if err != nil {
		return nil, err
	}

	secret := "forq-benchmark-secret-at-least-32-chars"
	apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)

	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(),
		"FORQ_AUTH_SECRET="+secret,
		"FORQ_DB_PATH="+filepath.Join(workDir, "forq.db"),
		"FORQ_ENV=local",
		"FORQ_API_ADDR="+apiAddr,
		fmt.Sprintf("FORQ_UI_ADDR=127.0.0.1:%d", uiPort),
	)
	logFile, err := os.Create(filepath.Join(workDir, "forq.log"))
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start forq: %w", err)
	}

	srv := &managedServer{
		cmd:        cmd,
		apiURL:     "http://" + apiAddr,
		authSecret: secret,
		workDir:    workDir,
	}

	// wait for healthcheck
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.apiURL + "/healthcheck")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent {
				fmt.Printf("✅ managed forq server ready on %s (logs: %s)\n\n", apiAddr, logFile.Name())
				return srv, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	srv.stop()
	return nil, fmt.Errorf("forq did not become healthy within 10s, see %s", logFile.Name())
}

func (s *managedServer) stop() {
	if s.cmd.Process != nil {
		s.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			s.cmd.Process.Kill()
		}
	}
	os.RemoveAll(s.workDir)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// ---------------------------------------------------------------------------
// runner
// ---------------------------------------------------------------------------

type runner struct {
	cfg        *config
	httpClient *http.Client

	// recording flips from false (warmup) to true (measurement window); all
	// counters and samples are gated on it
	recording atomic.Bool

	produced   atomic.Int64
	consumed   atomic.Int64
	emptyPolls atomic.Int64

	produceErrors atomic.Int64
	consumeErrors atomic.Int64
	ackErrors     atomic.Int64

	// duplicate delivery detection across all consumers
	seenIDs    sync.Map // message id -> struct{}
	duplicates atomic.Int64

	produceLat *latencyRecorder
	consumeLat *latencyRecorder
	ackLat     *latencyRecorder
	e2eLat     *latencyRecorder

	errsMu sync.Mutex
	errs   []string
}

func newRunner(cfg *config) *runner {
	var transport http.RoundTripper
	if cfg.UseHTTP2 {
		// h2c: HTTP/2 over cleartext TCP, matching Forq's recommended setup
		transport = &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
	} else {
		transport = &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	return &runner{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   40 * time.Second, // must exceed the 30s long poll
			Transport: transport,
		},
		produceLat: newLatencyRecorder(),
		consumeLat: newLatencyRecorder(),
		ackLat:     newLatencyRecorder(),
		e2eLat:     newLatencyRecorder(),
	}
}

func (r *runner) healthcheck() error {
	resp, err := r.httpClient.Get(r.cfg.APIURL + "/healthcheck")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("healthcheck returned %d", resp.StatusCode)
	}
	return nil
}

// payload carries the produce timestamp for end-to-end latency measurement,
// padded to the configured size. Backlog messages carry sentAtMs=0 so their
// (meaningless) e2e latency is not recorded.
type payload struct {
	SentAtMs int64  `json:"sentAtMs"`
	Pad      string `json:"pad"`
}

func (r *runner) buildContent(sentAtMs int64) string {
	base, _ := json.Marshal(payload{SentAtMs: sentAtMs, Pad: ""})
	padLen := r.cfg.Size - len(base)
	if padLen < 0 {
		padLen = 0
	}
	content, _ := json.Marshal(payload{SentAtMs: sentAtMs, Pad: strings.Repeat("x", padLen)})
	return string(content)
}

func (r *runner) seedBacklog() error {
	if r.cfg.Backlog == 0 {
		return nil
	}
	fmt.Printf("🌱 seeding backlog of %d messages...\n", r.cfg.Backlog)

	const seeders = 10
	var wg sync.WaitGroup
	errCh := make(chan error, seeders)
	perSeeder := r.cfg.Backlog / seeders
	remainder := r.cfg.Backlog % seeders

	for i := 0; i < seeders; i++ {
		n := perSeeder
		if i < remainder {
			n++
		}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				if _, err := r.produce(0); err != nil {
					errCh <- err
					return
				}
			}
		}(n)
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return err
	}
	fmt.Printf("✅ backlog seeded\n\n")
	return nil
}

func (r *runner) run() *results {
	sc := scenarios[r.cfg.Scenario]
	total := r.cfg.Warmup + r.cfg.Duration
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()

	var startTime time.Time
	go func() {
		select {
		case <-time.After(r.cfg.Warmup):
			startTime = time.Now()
			r.recording.Store(true)
			if r.cfg.Warmup > 0 {
				fmt.Printf("✅ warmup complete, measurement window started\n\n")
			}
		case <-ctx.Done():
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < sc.Consumers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r.runConsumer(ctx, fmt.Sprintf("consumer-%d", id))
		}(i)
	}
	for i := 0; i < sc.Producers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r.runProducer(ctx, fmt.Sprintf("producer-%d", id))
		}(i)
	}
	wg.Wait()
	endTime := time.Now()
	if startTime.IsZero() {
		startTime = endTime.Add(-r.cfg.Duration)
	}

	window := endTime.Sub(startTime).Seconds()
	return &results{
		Config:            r.cfg,
		Protocol:          protocolName(r.cfg.UseHTTP2),
		WindowSeconds:     window,
		Produced:          r.produced.Load(),
		Consumed:          r.consumed.Load(),
		EmptyPolls:        r.emptyPolls.Load(),
		Duplicates:        r.duplicates.Load(),
		ProduceErrors:     r.produceErrors.Load(),
		ConsumeErrors:     r.consumeErrors.Load(),
		AckErrors:         r.ackErrors.Load(),
		ProducedPerSecond: float64(r.produced.Load()) / window,
		ConsumedPerSecond: float64(r.consumed.Load()) / window,
		ProduceLatency:    r.produceLat.summary(),
		ConsumeLatency:    r.consumeLat.summary(),
		AckLatency:        r.ackLat.summary(),
		EndToEndLatency:   r.e2eLat.summary(),
	}
}

func (r *runner) runProducer(ctx context.Context, id string) {
	var ticker *time.Ticker
	if r.cfg.Rate > 0 {
		ticker = time.NewTicker(time.Duration(float64(time.Second) / r.cfg.Rate))
		defer ticker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if ticker != nil {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}

		start := time.Now()
		_, err := r.produce(time.Now().UnixMilli())
		recording := r.recording.Load()
		if err != nil {
			if recording {
				r.produceErrors.Add(1)
				r.logError("produce", id, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if recording {
			r.produced.Add(1)
			r.produceLat.record(time.Since(start))
		}
	}
}

func (r *runner) runConsumer(ctx context.Context, id string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// the consume timer covers ONLY this HTTP round trip; empty polls are
		// counted but not recorded as latency, so waiting for producers can't
		// masquerade as system latency
		start := time.Now()
		msg, err := r.consume(ctx)
		consumeRTT := time.Since(start)
		recording := r.recording.Load()

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if recording {
				r.consumeErrors.Add(1)
				r.logError("consume", id, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if msg == nil {
			if recording {
				r.emptyPolls.Add(1)
			}
			continue
		}

		nowMs := time.Now().UnixMilli()
		if recording {
			r.consumeLat.record(consumeRTT)

			if _, loaded := r.seenIDs.LoadOrStore(msg.ID, struct{}{}); loaded {
				r.duplicates.Add(1)
			}

			var p payload
			if err := json.Unmarshal([]byte(msg.Content), &p); err == nil && p.SentAtMs > 0 {
				r.e2eLat.record(time.Duration(nowMs-p.SentAtMs) * time.Millisecond)
			}
		}

		ackStart := time.Now()
		err = r.ack(msg.ID, msg.Receipt)
		if recording {
			if err != nil {
				r.ackErrors.Add(1)
				r.logError("ack", id, err)
			} else {
				r.ackLat.record(time.Since(ackStart))
				r.consumed.Add(1)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP operations
// ---------------------------------------------------------------------------

type produceRequest struct {
	Content string `json:"content"`
}

type consumeResponse struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Receipt string `json:"receipt"`
}

func (r *runner) produce(sentAtMs int64) (string, error) {
	body, _ := json.Marshal(produceRequest{Content: r.buildContent(sentAtMs)})
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/v1/queues/%s/messages", r.cfg.APIURL, r.cfg.QueueName),
		strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", r.cfg.Auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("produce returned %d", resp.StatusCode)
	}
	return "", nil
}

func (r *runner) consume(ctx context.Context) (*consumeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v1/queues/%s/messages", r.cfg.APIURL, r.cfg.QueueName), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", r.cfg.Auth)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var msg consumeResponse
		if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
			return nil, err
		}
		return &msg, nil
	default:
		return nil, fmt.Errorf("consume returned %d", resp.StatusCode)
	}
}

func (r *runner) ack(messageID, receipt string) error {
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/v1/queues/%s/messages/%s/ack", r.cfg.APIURL, r.cfg.QueueName, messageID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", r.cfg.Auth)
	req.Header.Set(receiptHeader, receipt)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ack returned %d", resp.StatusCode)
	}
	return nil
}

func drainAndClose(body io.ReadCloser) {
	io.Copy(io.Discard, body)
	body.Close()
}

func (r *runner) logError(op, worker string, err error) {
	r.errsMu.Lock()
	defer r.errsMu.Unlock()
	if len(r.errs) < 20 {
		r.errs = append(r.errs, fmt.Sprintf("[%s] %s: %v", worker, op, err))
	}
}

func (r *runner) printErrors() {
	r.errsMu.Lock()
	defer r.errsMu.Unlock()
	if len(r.errs) == 0 {
		return
	}
	fmt.Printf("\n⚠️  errors (first %d):\n", len(r.errs))
	for _, e := range r.errs {
		fmt.Println("  " + e)
	}
}

// ---------------------------------------------------------------------------
// metrics
// ---------------------------------------------------------------------------

type latencyRecorder struct {
	mu      sync.Mutex
	samples []time.Duration
}

func newLatencyRecorder() *latencyRecorder {
	return &latencyRecorder{samples: make([]time.Duration, 0, 100_000)}
}

func (lr *latencyRecorder) record(d time.Duration) {
	lr.mu.Lock()
	lr.samples = append(lr.samples, d)
	lr.mu.Unlock()
}

type latencySummary struct {
	Count int64   `json:"count"`
	AvgMs float64 `json:"avgMs"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
	MaxMs float64 `json:"maxMs"`
}

func (lr *latencyRecorder) summary() latencySummary {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if len(lr.samples) == 0 {
		return latencySummary{}
	}
	sorted := make([]time.Duration, len(lr.samples))
	copy(sorted, lr.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	pct := func(p float64) float64 {
		idx := int(float64(len(sorted)-1) * p)
		return float64(sorted[idx].Microseconds()) / 1000
	}
	return latencySummary{
		Count: int64(len(sorted)),
		AvgMs: float64((sum / time.Duration(len(sorted))).Microseconds()) / 1000,
		P50Ms: pct(0.50),
		P95Ms: pct(0.95),
		P99Ms: pct(0.99),
		MaxMs: float64(sorted[len(sorted)-1].Microseconds()) / 1000,
	}
}

type results struct {
	Config            *config        `json:"config"`
	Protocol          string         `json:"protocol"`
	WindowSeconds     float64        `json:"windowSeconds"`
	Produced          int64          `json:"produced"`
	Consumed          int64          `json:"consumed"`
	EmptyPolls        int64          `json:"emptyPolls"`
	Duplicates        int64          `json:"duplicates"`
	ProduceErrors     int64          `json:"produceErrors"`
	ConsumeErrors     int64          `json:"consumeErrors"`
	AckErrors         int64          `json:"ackErrors"`
	ProducedPerSecond float64        `json:"producedPerSecond"`
	ConsumedPerSecond float64        `json:"consumedPerSecond"`
	ProduceLatency    latencySummary `json:"produceLatency"`
	ConsumeLatency    latencySummary `json:"consumeLatency"`
	AckLatency        latencySummary `json:"ackLatency"`
	EndToEndLatency   latencySummary `json:"endToEndLatency"`
}

func printResults(res *results) {
	fmt.Printf("\n📊 Results (%s, %.1fs measurement window)\n", res.Protocol, res.WindowSeconds)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Throughput:  %8.1f produced/sec   %8.1f consumed/sec\n", res.ProducedPerSecond, res.ConsumedPerSecond)
	fmt.Printf("Totals:      %8d produced       %8d consumed (%d empty polls)\n", res.Produced, res.Consumed, res.EmptyPolls)
	fmt.Printf("Errors:      %8d produce  %d consume  %d ack\n", res.ProduceErrors, res.ConsumeErrors, res.AckErrors)
	fmt.Printf("Duplicates:  %8d (expected 0 unless consumers exceed the visibility timeout)\n\n", res.Duplicates)

	printLatency := func(name string, s latencySummary) {
		if s.Count == 0 {
			fmt.Printf("%-12s (no samples)\n", name)
			return
		}
		fmt.Printf("%-12s avg %8.2fms   p50 %8.2fms   p95 %8.2fms   p99 %8.2fms   max %8.2fms   (n=%d)\n",
			name, s.AvgMs, s.P50Ms, s.P95Ms, s.P99Ms, s.MaxMs, s.Count)
	}
	printLatency("produce", res.ProduceLatency)
	printLatency("consume", res.ConsumeLatency)
	printLatency("ack", res.AckLatency)
	printLatency("end-to-end", res.EndToEndLatency)
	fmt.Printf("\nend-to-end = produce timestamp -> consumed; with a backlog it is dominated\nby queue depth (FIFO: new messages wait behind the backlog), which is honest\nqueue behavior, not overhead.\n")
}

func writeJSON(path string, res *results) {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		log.Fatalf("failed to marshal results: %v", err)
	}
	data = append(data, '\n')
	if path == "-" {
		os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Fatalf("failed to write results: %v", err)
	}
	fmt.Printf("\n💾 results written to %s\n", path)
}
