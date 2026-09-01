package utils

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		trustProxy bool
		want       string
	}{
		{
			name:       "direct connection, no proxy trust",
			remoteAddr: "203.0.113.7:51234",
			want:       "203.0.113.7",
		},
		{
			name:       "XFF ignored when proxy not trusted",
			remoteAddr: "10.0.0.1:80",
			xff:        "203.0.113.7",
			want:       "10.0.0.1",
		},
		{
			name:       "XFF used when proxy trusted",
			remoteAddr: "10.0.0.1:80",
			xff:        "203.0.113.7",
			trustProxy: true,
			want:       "203.0.113.7",
		},
		{
			name:       "rightmost XFF entry wins (proxy-appended)",
			remoteAddr: "10.0.0.1:80",
			xff:        "1.2.3.4, 203.0.113.7",
			trustProxy: true,
			want:       "203.0.113.7",
		},
		{
			name:       "malformed XFF falls back to RemoteAddr",
			remoteAddr: "10.0.0.1:80",
			xff:        "not-an-ip",
			trustProxy: true,
			want:       "10.0.0.1",
		},
		{
			name:       "empty XFF falls back to RemoteAddr",
			remoteAddr: "10.0.0.1:80",
			xff:        "",
			trustProxy: true,
			want:       "10.0.0.1",
		},
		{
			name:       "IPv6 remote addr",
			remoteAddr: "[2001:db8::1]:443",
			want:       "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			if got := ClientIP(req, tt.trustProxy); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A proxy that emits its OWN X-Forwarded-For line rather than appending to the
// client's arrives as two header field lines. Header.Get returns only the
// first (attacker-controlled) line, so ClientIP must iterate Header.Values and
// take the rightmost parseable entry across all lines.
func TestClientIPSeparateProxyLine(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = "10.0.0.1:80"
	req.Header.Add("X-Forwarded-For", "1.2.3.4")     // spoofed by the client
	req.Header.Add("X-Forwarded-For", "203.0.113.7") // added by the proxy

	if got := ClientIP(req, true); got != "203.0.113.7" {
		t.Errorf("ClientIP() = %q, want %q (proxy-added rightmost value)", got, "203.0.113.7")
	}
}
