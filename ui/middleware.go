package ui

import (
	"net/http"
	"strings"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/services"

	"github.com/go-chi/chi/v5"
	"github.com/justinas/nosurf"
)

// securityHeaders middleware sets HTTP security headers on every UI response.
// All assets are served from the embedded static FS, so the CSP allows only
// 'self' - no CDNs, no inline scripts or styles.
func securityHeaders(env string) func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"frame-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			// authenticated pages render message content and failure reasons;
			// no-store keeps them out of the browser disk cache, so they can't
			// be viewed via back/forward after logout on a shared machine.
			// The /static/ handler overrides this for the embedded assets.
			h.Set("Cache-Control", "no-store")
			if env == common.ProEnv {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, req)
		})
	}
}

// maxUIBodyBytes caps UI request bodies. The only UI endpoint that reads a
// body is /login (a small form); without this it falls back to net/http's
// 10 MB form default, an unauthenticated 10 MB parse per request.
const maxUIBodyBytes = 64 << 10

// bodyLimit wraps the request body so a read past the cap fails instead of
// buffering unboundedly. Applied before processLogin's ParseForm.
func bodyLimit(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Body != nil {
				req.Body = http.MaxBytesReader(w, req.Body, max)
			}
			next.ServeHTTP(w, req)
		})
	}
}

// validateQueueName rejects queue-name URL segments outside the allowed
// charset before they reach templates or destructive handlers.
func validateQueueName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !common.IsValidQueueName(chi.URLParam(req, "queue")) {
			http.NotFound(w, req)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// sessionAuth middleware for UI routes
func sessionAuth(sessionsService *services.SessionsService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			sessionCookie, err := req.Cookie("ForqSession")
			if err != nil {
				http.Redirect(w, req, "/login", http.StatusFound)
				return
			}

			sessionId := sessionCookie.Value
			if !sessionsService.IsSessionValid(sessionId) {
				http.Redirect(w, req, "/login", http.StatusFound)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// csrfPrevention middleware to protect against CSRF attacks
func csrfPrevention(csrfFailureHandler func(w http.ResponseWriter, r *http.Request), env string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		csrfHandler := nosurf.New(next)
		csrfHandler.SetBaseCookie(http.Cookie{
			HttpOnly: true,
			Path:     "/",
			Secure:   env == common.ProEnv,
			SameSite: http.SameSiteLaxMode,
		})

		csrfHandler.SetFailureHandler(http.HandlerFunc(csrfFailureHandler))

		// we are using HTTP in local env, so we need to disable the Secure flag check
		if env == common.LocalEnv {
			csrfHandler.SetIsTLSFunc(func(r *http.Request) bool {
				return false
			})
		}
		return csrfHandler
	}
}
