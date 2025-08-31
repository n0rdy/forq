package ui

import (
	"forq/common"
	"forq/services"
	"net/http"

	"github.com/justinas/nosurf"
)

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
