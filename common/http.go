package common

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the client IP for a request.
//
// When trustProxyHeaders is false (default), only RemoteAddr is used. This is
// safe when Forq is exposed directly to clients.
//
// When true, the rightmost entry of X-Forwarded-For is used (the IP the proxy
// adds for the connecting client), falling back to X-Real-IP and then to
// RemoteAddr. Set FORQ_TRUST_PROXY_HEADERS=true ONLY when Forq is behind a
// reverse proxy that strips or replaces incoming forwarded headers — otherwise
// attackers can spoof their IP and bypass throttling. Assumes a single proxy
// hop; multi-hop deployments should canonicalize the header at the edge proxy
// before it reaches Forq.
func ClientIP(req *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			rightmost := strings.TrimSpace(parts[len(parts)-1])
			if ip := net.ParseIP(rightmost); ip != nil {
				return ip.String()
			}
		}
		if xri := strings.TrimSpace(req.Header.Get("X-Real-IP")); xri != "" {
			if ip := net.ParseIP(xri); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
