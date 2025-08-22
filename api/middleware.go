package api

import (
	"encoding/json"
	"forq/common"
	"forq/services"
	"net/http"

	"github.com/rs/zerolog/log"
)

var (
	unauthorizedRespBody []byte
)

func init() {
	var err error
	unauthorizedRespBody, err = json.Marshal(common.ErrorResponse{Code: common.ErrCodeUnauthorized})
	if err != nil {
		panic(err)
	}
}

func bearerTokenAuth(authSecret string) func(http.Handler) http.Handler {
	expectedHeader := "Bearer " + authSecret
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authHeader := req.Header.Get("Authorization")
			if authHeader != expectedHeader {
				log.Error().Msg("Invalid bearer token")
				sendUnauthorizedErrorResponse(w)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

func sessionAuth(sessionsService *services.SessionsService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			sessionCookie, err := req.Cookie("ForqSession")
			if err != nil {
				log.Error().Err(err).Msg("Failed to get ForqSession cookie")
				http.Redirect(w, req, "/ui/login", http.StatusFound)
				return
			}

			sessionId := sessionCookie.Value
			if !sessionsService.IsSessionValid(sessionId) {
				log.Error().Msg("Invalid session ID: " + sessionId)
				http.Redirect(w, req, "/ui/login", http.StatusFound)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

func sendUnauthorizedErrorResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(401)
	w.Write(unauthorizedRespBody)
}
