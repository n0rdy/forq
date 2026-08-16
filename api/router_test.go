package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n0rdy/forq/api"
	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/internal/testutil"
	"github.com/n0rdy/forq/metrics"
	"github.com/n0rdy/forq/services"
)

const testAuthSecret = "test-secret-that-is-32-chars-long"

// newTestServer spins up the full API stack (real router, services, and
// SQLite repo) behind an httptest server. Each call gets its own throttling
// service, so lockout state can't leak between tests.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	repo, appConfigs, _ := testutil.NewTestRepo(t)
	metricsService := metrics.NewMetricsService(false)
	messagesService := services.NewMessagesService(metricsService, repo, appConfigs)
	monitoringService := services.NewMonitoringService(repo)
	throttlingService := services.NewThrottlingService()
	t.Cleanup(func() { throttlingService.Close() })

	router := api.NewRouter(monitoringService, messagesService, throttlingService, testAuthSecret, false, "", common.LocalEnv, false)
	srv := httptest.NewServer(router.NewRouter())
	t.Cleanup(srv.Close)
	return srv
}

func doRequest(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", testAuthSecret)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, sb.String()
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var er common.ErrorResponse
	if err := json.Unmarshal([]byte(body), &er); err != nil {
		t.Fatalf("response body is not an ErrorResponse envelope: %q", body)
	}
	return er.Code
}

func TestProduceConsumeAckLifecycle(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/api/v1/queues/orders/messages"

	resp, _ := doRequest(t, "POST", base, `{"content":"hello"}`, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("produce: %d", resp.StatusCode)
	}

	resp, body := doRequest(t, "GET", base, "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consume: %d %s", resp.StatusCode, body)
	}
	var msg common.MessageResponse
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Id == "" || msg.Content != "hello" || msg.Receipt == "" {
		t.Fatalf("consume response incomplete: %+v", msg)
	}

	// ack without receipt -> 400 with the dedicated "outdated client" code
	resp, body = doRequest(t, "POST", base+"/"+msg.Id+"/ack", "", nil)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != common.ErrCodeBadRequestReceiptMissing {
		t.Fatalf("ack without receipt: %d %s", resp.StatusCode, body)
	}

	// ack with a stale/wrong receipt -> 404
	resp, body = doRequest(t, "POST", base+"/"+msg.Id+"/ack", "", map[string]string{common.ReceiptHeader: "12345"})
	if resp.StatusCode != http.StatusNotFound || errorCode(t, body) != common.ErrCodeNotFoundMessage {
		t.Fatalf("ack with wrong receipt: %d %s", resp.StatusCode, body)
	}

	// ack with the real receipt -> 204
	resp, _ = doRequest(t, "POST", base+"/"+msg.Id+"/ack", "", map[string]string{common.ReceiptHeader: msg.Receipt})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", resp.StatusCode)
	}

	// double ack -> 404
	resp, _ = doRequest(t, "POST", base+"/"+msg.Id+"/ack", "", map[string]string{common.ReceiptHeader: msg.Receipt})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("double ack: %d", resp.StatusCode)
	}
}

func TestNackSchedulesRetry(t *testing.T) {
	srv := newTestServer(t)
	base := srv.URL + "/api/v1/queues/orders/messages"

	doRequest(t, "POST", base, `{"content":"retry-me"}`, nil)
	_, body := doRequest(t, "GET", base, "", nil)
	var msg common.MessageResponse
	json.Unmarshal([]byte(body), &msg)

	resp, _ := doRequest(t, "POST", base+"/"+msg.Id+"/nack", "", map[string]string{common.ReceiptHeader: msg.Receipt})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("nack: %d", resp.StatusCode)
	}

	// nacking again with the now-stale receipt -> 404
	resp, _ = doRequest(t, "POST", base+"/"+msg.Id+"/nack", "", map[string]string{common.ReceiptHeader: msg.Receipt})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stale nack: %d", resp.StatusCode)
	}
}

func TestProduceValidation(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name     string
		queue    string
		body     string
		wantCode int
		wantErr  string
	}{
		{"invalid JSON", "orders", "{not-json", http.StatusBadRequest, common.ErrCodeBadRequestInvalidBody},
		{"produce into DLQ", "orders-dlq", `{"content":"x"}`, http.StatusBadRequest, common.ErrCodeBadRequestProduceToDlq},
		{"invalid queue name", "or%23ders", `{"content":"x"}`, http.StatusBadRequest, common.ErrCodeBadRequestInvalidQueueName},
		{"queue name too long", strings.Repeat("a", 65), `{"content":"x"}`, http.StatusBadRequest, common.ErrCodeBadRequestInvalidQueueName},
		{"content over 256KB", "orders", fmt.Sprintf(`{"content":%q}`, strings.Repeat("x", 256*1024+1)), http.StatusBadRequest, common.ErrCodeBadRequestContentExceedsLimit},
		{"processAfter in past", "orders", `{"content":"x","processAfter":1000}`, http.StatusBadRequest, common.ErrCodeBadRequestProcessAfterInPast},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := doRequest(t, "POST", srv.URL+"/api/v1/queues/"+tt.queue+"/messages", tt.body, nil)
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tt.wantCode, body)
			}
			if got := errorCode(t, body); got != tt.wantErr {
				t.Fatalf("code = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestProduceBodySizeLimit(t *testing.T) {
	srv := newTestServer(t)

	// over the MaxBytesReader cap (2MB) -> 413 before decoding
	huge := fmt.Sprintf(`{"content":%q}`, strings.Repeat("x", 3*1024*1024))
	resp, body := doRequest(t, "POST", srv.URL+"/api/v1/queues/orders/messages", huge, nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("3MB body: %d %s", resp.StatusCode, body)
	}
	if errorCode(t, body) != common.ErrCodeBadRequestContentExceedsLimit {
		t.Fatalf("code = %q", errorCode(t, body))
	}
}

func TestMessageIdValidation(t *testing.T) {
	srv := newTestServer(t)

	resp, body := doRequest(t, "POST", srv.URL+"/api/v1/queues/orders/messages/not-a-uuid/ack", "",
		map[string]string{common.ReceiptHeader: "123"})
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != common.ErrCodeBadRequestInvalidMessageId {
		t.Fatalf("non-UUID messageId: %d %s", resp.StatusCode, body)
	}
}

func TestAuth(t *testing.T) {
	srv := newTestServer(t)
	url := srv.URL + "/api/v1/queues/orders/messages"

	req, _ := http.NewRequest("POST", url, strings.NewReader(`{"content":"x"}`))
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", url, strings.NewReader(`{"content":"x"}`))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", resp.StatusCode)
	}
}

// TestThrottleLockoutDoesNotBlockValidKey pins the check order: the lockout
// only ever applies to failed attempts, so a valid key keeps working even
// when its IP is locked out (critical behind a shared-IP reverse proxy).
func TestThrottleLockoutDoesNotBlockValidKey(t *testing.T) {
	srv := newTestServer(t)
	url := srv.URL + "/api/v1/queues/orders/messages"

	// trip the lockout with bogus keys
	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest("POST", url, strings.NewReader(`{"content":"x"}`))
		req.Header.Set("X-API-Key", "bogus")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if i >= 5 && resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("request %d with bogus key: %d, want 429 once locked", i+1, resp.StatusCode)
		}
	}

	// valid key still passes
	resp, _ := doRequest(t, "POST", url, `{"content":"x"}`, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("valid key while IP locked: %d, want 204", resp.StatusCode)
	}
}

func TestHealthcheck(t *testing.T) {
	srv := newTestServer(t)

	// healthcheck needs no auth
	resp, err := http.Get(srv.URL + "/healthcheck")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("healthcheck: %d", resp.StatusCode)
	}
}
