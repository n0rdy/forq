package ui

import (
	"forq/services"
	"net/http"
)

// sessionAuth middleware for UI routes
func sessionAuth(sessionsService *services.SessionsService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			sessionCookie, err := req.Cookie("ForqSession")
			if err != nil {
				http.Redirect(w, req, "/ui/login", http.StatusFound)
				return
			}

			sessionId := sessionCookie.Value
			if !sessionsService.IsSessionValid(sessionId) {
				http.Redirect(w, req, "/ui/login", http.StatusFound)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}
