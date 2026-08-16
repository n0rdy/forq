package ui_test

import (
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/internal/testutil"
	"github.com/n0rdy/forq/metrics"
	"github.com/n0rdy/forq/services"
	"github.com/n0rdy/forq/ui"
)

const testAuthSecret = "test-secret-that-is-32-chars-long"

func newUITestServer(t *testing.T) *httptest.Server {
	t.Helper()

	repo, appConfigs, _ := testutil.NewTestRepo(t)
	metricsService := metrics.NewMetricsService(false)
	messagesService := services.NewMessagesService(metricsService, repo, appConfigs)
	queuesService := services.NewQueuesService(repo)
	sessionsService := services.NewSessionsService()
	t.Cleanup(func() { sessionsService.Close() })
	throttlingService := services.NewThrottlingService()
	t.Cleanup(func() { throttlingService.Close() })

	router := ui.NewRouter(messagesService, sessionsService, queuesService, throttlingService, testAuthSecret, common.LocalEnv, false)
	srv := httptest.NewServer(router.NewRouter())
	t.Cleanup(srv.Close)
	return srv
}

// newClientWithJar returns an HTTP client that keeps cookies and does NOT
// follow redirects, so tests can assert on 302s directly.
func newClientWithJar(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var csrfTokenRe = regexp.MustCompile(`X-CSRF-Token": "([^"]+)"`)

// login performs the full CSRF-protected login flow and returns the client
// holding the session cookie.
func login(t *testing.T, srv *httptest.Server, token string) (*http.Client, *http.Response) {
	t.Helper()
	client := newClientWithJar(t)

	resp, err := client.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	match := csrfTokenRe.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no CSRF token found in login page")
	}
	// the token sits in an HTML attribute, so html/template entity-escapes
	// characters like '+' - browsers unescape automatically, tests must too
	csrfToken := html.UnescapeString(match[1])

	form := url.Values{"token": {token}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", csrfToken)
	// nosurf (post-CVE-2025-46721) additionally enforces same-origin via
	// Sec-Fetch-Site/Origin/Referer; browsers always send Origin on POST
	req.Header.Set("Origin", srv.URL)

	loginResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	return client, loginResp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
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
	return sb.String()
}

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	srv := newUITestServer(t)
	client := newClientWithJar(t)

	for _, path := range []string{"/", "/queue/orders"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login" {
			t.Fatalf("%s: status=%d location=%q, want 302 to /login", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

func TestSecurityAndCacheHeaders(t *testing.T) {
	srv := newUITestServer(t)

	resp, err := http.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP missing default-src 'self': %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "cdn.") {
		t.Errorf("CSP must not allow inline scripts or CDNs: %q", csp)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("page Cache-Control = %q, want no-store", cc)
	}

	// static assets are served from the binary and cacheable
	for _, asset := range []string{"/static/styles.css", "/static/htmx.min.js", "/static/theme.js"} {
		resp, err := http.Get(srv.URL + asset)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", asset, resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "public") {
			t.Errorf("%s: Cache-Control = %q, want public caching", asset, cc)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	srv := newUITestServer(t)

	// wrong token -> 401, no session cookie
	_, resp := login(t, srv, "wrong-token")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d, want 401", resp.StatusCode)
	}

	// correct token -> 200 + HX-Redirect + session cookie grants access
	client, resp := login(t, srv, testAuthSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	if resp.Header.Get("HX-Redirect") != "/" {
		t.Fatalf("HX-Redirect = %q", resp.Header.Get("HX-Redirect"))
	}

	dashResp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	dashResp.Body.Close()
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard after login: %d", dashResp.StatusCode)
	}
}

func TestLoginRequiresCSRFToken(t *testing.T) {
	srv := newUITestServer(t)
	client := newClientWithJar(t)

	form := url.Values{"token": {testAuthSecret}}
	resp, err := client.PostForm(srv.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// no CSRF token -> the failure handler redirects to /login; the login
	// must NOT succeed
	if resp.StatusCode == http.StatusOK && resp.Header.Get("HX-Redirect") != "" {
		t.Fatal("login succeeded without a CSRF token")
	}
}

func TestQueueNameValidation(t *testing.T) {
	srv := newUITestServer(t)
	client, _ := login(t, srv, testAuthSecret)

	resp, err := client.Get(srv.URL + "/queue/bad%23name")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid queue name: %d, want 404", resp.StatusCode)
	}
}
