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

type Router struct {
	messagesService *services.MessagesService
	authSecret      string
}

func NewRouter(messagesService *services.MessagesService, authSecret string) *Router {
	return &Router{
		messagesService: messagesService,
		authSecret:      authSecret,
	}
}

func (ar *Router) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Get("/healthcheck", ar.healthcheck)

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(bearerTokenAuth(ar.authSecret))

		r.Route("/queues", func(r chi.Router) {
			r.Route("/{queue}/messages", func(r chi.Router) {
				r.Post("/", ar.sendMessage)
				r.Get("/", ar.fetchMessage)

				r.Route("/{messageId}", func(r chi.Router) {
					r.Post("/ack", ar.ackMessage)
					r.Post("/nack", ar.nackMessage)
				})
			})
		})
	})

	return router
}

func (ar *Router) sendMessage(w http.ResponseWriter, req *http.Request) {
	var newMessage common.NewMessageRequest
	err := json.NewDecoder(req.Body).Decode(&newMessage)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decode request body")
		ar.sendErrorResponse(w, http.StatusBadRequest, common.ErrCodeBadRequestInvalidBody)
		return
	}

	queueName := chi.URLParam(req, "queue")

	err = ar.messagesService.ProcessNewMessage(newMessage, queueName, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) fetchMessage(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	message, err := ar.messagesService.FetchMessage(queueName, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	if message == nil {
		ar.sendNoContentEmptyResponse(w)
		return
	}
	ar.sendJsonResponse(w, http.StatusOK, message)
}

func (ar *Router) ackMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := ar.messagesService.AckMessage(messageId, queueName, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) nackMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := ar.messagesService.NackMessage(messageId, queueName, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) healthcheck(w http.ResponseWriter, req *http.Request) {
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) sendNoContentEmptyResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (ar *Router) sendJsonResponse(w http.ResponseWriter, httpCode int, payload interface{}) {
	respBody, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Error marshaling response body")
		ar.sendErrorResponse(w, http.StatusInternalServerError, common.ErrCodeInternal)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	w.Write(respBody)
}

func (ar *Router) sendErrorResponse(w http.ResponseWriter, httpCode int, errCode string) {
	ar.sendJsonResponse(w, httpCode, common.ErrorResponse{Code: errCode})
}

func (ar *Router) sendResponseFromError(w http.ResponseWriter, err error) {
	var fe *common.ForqError
	if errors.As(err, &fe) {
		ar.sendJsonResponse(w, http.StatusMultiStatus, fe.Code)
	} else {
		ar.sendErrorResponse(w, http.StatusInternalServerError, common.ErrCodeInternal)
	}
}
