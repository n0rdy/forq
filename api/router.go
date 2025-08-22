package api

import (
	"encoding/json"
	"errors"
	"forq/common"
	"forq/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type ForqRouter struct {
	messagesService *services.MessagesService
	sessionsService *services.SessionsService
	authSecret      string
}

func NewForqRouter(messagesService *services.MessagesService, sessionsService *services.SessionsService, authSecret string) *ForqRouter {
	return &ForqRouter{
		messagesService: messagesService,
		sessionsService: sessionsService,
		authSecret:      authSecret,
	}
}

func (fr *ForqRouter) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Get("/healthcheck", fr.healthcheck)

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(bearerTokenAuth(fr.authSecret))

		r.Route("/queues", func(r chi.Router) {
			r.Route("/{queue}/messages", func(r chi.Router) {
				r.Post("/", fr.sendMessage)
				r.Get("/", fr.fetchMessage)

				r.Route("/{messageId}", func(r chi.Router) {
					r.Post("/ack", fr.ackMessage)
					r.Post("/nack", fr.nackMessage)
				})
			})
		})
	})

	router.Route("/ui", func(r chi.Router) {
		r.Use(sessionAuth(fr.sessionsService))

	})

	return router
}

func (fr *ForqRouter) sendMessage(w http.ResponseWriter, req *http.Request) {
	var newMessage common.NewMessageRequest
	err := json.NewDecoder(req.Body).Decode(&newMessage)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decode request body")
		fr.sendErrorResponse(w, http.StatusBadRequest, common.ErrCodeBadRequestInvalidBody)
		return
	}

	queueName := chi.URLParam(req, "queue")

	err = fr.messagesService.ProcessNewMessage(newMessage, queueName, req.Context())
	if err != nil {
		fr.sendResponseFromError(w, err)
		return
	}
	fr.sendNoContentEmptyResponse(w)
}

func (fr *ForqRouter) fetchMessage(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	message, err := fr.messagesService.FetchMessage(queueName, req.Context())
	if err != nil {
		fr.sendResponseFromError(w, err)
		return
	}
	if message == nil {
		fr.sendNoContentEmptyResponse(w)
		return
	}
	fr.sendJsonResponse(w, http.StatusOK, message)
}

func (fr *ForqRouter) ackMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := fr.messagesService.AckMessage(messageId, queueName, req.Context())
	if err != nil {
		fr.sendResponseFromError(w, err)
		return
	}
	fr.sendNoContentEmptyResponse(w)
}

func (fr *ForqRouter) nackMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := fr.messagesService.NackMessage(messageId, queueName, req.Context())
	if err != nil {
		fr.sendResponseFromError(w, err)
		return
	}
	fr.sendNoContentEmptyResponse(w)
}

func (fr *ForqRouter) healthcheck(w http.ResponseWriter, req *http.Request) {
	fr.sendNoContentEmptyResponse(w)
}

func (fr *ForqRouter) sendNoContentEmptyResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (fr *ForqRouter) sendJsonResponse(w http.ResponseWriter, httpCode int, payload interface{}) {
	respBody, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Error marshaling response body")
		fr.sendErrorResponse(w, http.StatusInternalServerError, common.ErrCodeInternal)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	w.Write(respBody)
}

func (fr *ForqRouter) sendErrorResponse(w http.ResponseWriter, httpCode int, errCode string) {
	fr.sendJsonResponse(w, httpCode, common.ErrorResponse{Code: errCode})
}

func (fr *ForqRouter) sendResponseFromError(w http.ResponseWriter, err error) {
	var fe *common.ForqError
	if errors.As(err, &fe) {
		fr.sendJsonResponse(w, http.StatusMultiStatus, fe.Code)
	} else {
		fr.sendErrorResponse(w, http.StatusInternalServerError, common.ErrCodeInternal)
	}
}
