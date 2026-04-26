package ui

import (
	"net/http"
	"strings"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/services"
	"github.com/n0rdy/forq/utils"

	"github.com/justinas/nosurf"
)

// securityHeaders middleware sets HTTP security headers on every UI response.
// CSP allows jsdelivr.net (CDN for DaisyUI, Tailwind, HTMX) and 'unsafe-inline'
// for inline <script>/<style> blocks and HTMX hx-on attributes.
func securityHeaders(env string) func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
		"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
		"img-src 'self' data:",
		"font-src 'self' https://cdn.jsdelivr.net data:",
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
			if env == common.ProEnv {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, req)
		})
	}
}

// loginThrottle middleware blocks login attempts from IPs that have exceeded
// the failure threshold. Renders the login page with a rate-limit message
// instead of just returning 429 with no body, so the UX is still recognizable.
func loginThrottle(throttlingService *services.ThrottlingService, trustProxyHeaders bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ip := utils.ClientIP(req, trustProxyHeaders)
			if throttlingService.IsLocked(ip) {
				data := common.LoginPageData{
					Title: "Login",
					Error: "Too many failed login attempts. Try again in a minute.",
				}
				RenderTemplateWithStatus(w, req, http.StatusTooManyRequests, "login.html", data)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
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
