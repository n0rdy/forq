package common

import (
	"net"
	"net/http"
)

// ClientIP returns the client IP from RemoteAddr with the port stripped.
// X-Forwarded-For is intentionally not honored: trusting forwarded headers
// without a configured trusted-proxy list is a footgun (an attacker can
// spoof the header to bypass throttling). If running behind a reverse
// proxy, do rate limiting at the proxy layer for per-real-client behavior.
func ClientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
