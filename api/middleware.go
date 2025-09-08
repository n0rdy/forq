package api

import (
	"encoding/json"
	"forq/common"
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

func apiKeyTokenAuth(authSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authHeader := req.Header.Get("X-API-Key")
			if authHeader != authSecret {
				log.Error().Msg("Invalid API key")
				sendUnauthorizedErrorResponse(w)
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
