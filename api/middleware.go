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

func sendUnauthorizedErrorResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(401)
	w.Write(unauthorizedRespBody)
}
