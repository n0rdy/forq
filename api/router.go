package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/services"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// maxProduceBodyBytes bounds the produce request body before JSON decoding.
// The content limit is 256KB, but JSON escaping can inflate a legit payload
// (worst case 6x for \uXXXX escapes), so the cap is generous; the exact
// content-size check still happens after decoding.
const maxProduceBodyBytes = 2 * 1024 * 1024

type Router struct {
	monitoringService *services.MonitoringService
	messagesService   *services.MessagesService
	throttlingService *services.ThrottlingService
	authSecret        string
	metricsEnabled    bool
	metricsAuthSecret string
	env               string
	trustProxyHeaders bool
}

func NewRouter(
	monitoringService *services.MonitoringService,
	messagesService *services.MessagesService,
	throttlingService *services.ThrottlingService,
	authSecret string,
	metricsEnabled bool,
	metricsAuthSecret string,
	env string,
	trustProxyHeaders bool,
) *Router {
	return &Router{
		monitoringService: monitoringService,
		messagesService:   messagesService,
		throttlingService: throttlingService,
		authSecret:        authSecret,
		metricsEnabled:    metricsEnabled,
		metricsAuthSecret: metricsAuthSecret,
		env:               env,
		trustProxyHeaders: trustProxyHeaders,
	}
}

func (ar *Router) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Use(securityHeaders(ar.env))

	router.Get("/healthcheck", ar.healthcheck)

	if ar.metricsEnabled {
		router.Route("/metrics", func(r chi.Router) {
			r.Use(apiKeyTokenAuth(ar.metricsAuthSecret, ar.throttlingService, ar.trustProxyHeaders))

			r.Get("/", promhttp.Handler().ServeHTTP)
		})
	}

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(apiKeyTokenAuth(ar.authSecret, ar.throttlingService, ar.trustProxyHeaders))

		r.Route("/queues", func(r chi.Router) {
			r.Route("/{queue}/messages", func(r chi.Router) {
				r.Use(ar.validateQueueName)

				r.Post("/", ar.produceMessage)
				r.Get("/", ar.consumeMessage)

				r.Route("/{messageId}", func(r chi.Router) {
					r.Use(ar.validateMessageId)

					r.Post("/ack", ar.ackMessage)
					r.Post("/nack", ar.nackMessage)
				})
			})
		})
	})

	return router
}

func (ar *Router) validateQueueName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !common.IsValidQueueName(chi.URLParam(req, "queue")) {
			ar.sendErrorResponse(w, http.StatusBadRequest, common.ErrCodeBadRequestInvalidQueueName)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (ar *Router) validateMessageId(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !common.IsValidMessageId(chi.URLParam(req, "messageId")) {
			ar.sendErrorResponse(w, http.StatusBadRequest, common.ErrCodeBadRequestInvalidMessageId)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (ar *Router) produceMessage(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxProduceBodyBytes)

	var newMessage common.NewMessageRequest
	err := json.NewDecoder(req.Body).Decode(&newMessage)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			log.Error().Err(err).Msg("Request body exceeds size limit")
			ar.sendErrorResponse(w, http.StatusRequestEntityTooLarge, common.ErrCodeBadRequestContentExceedsLimit)
			return
		}
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

func (ar *Router) consumeMessage(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	message, err := ar.messagesService.GetMessageForConsuming(queueName, req.Context())
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
	receipt := req.Header.Get(common.ReceiptHeader)

	err := ar.messagesService.AckMessage(messageId, queueName, receipt, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) nackMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")
	receipt := req.Header.Get(common.ReceiptHeader)

	err := ar.messagesService.NackMessage(messageId, queueName, receipt, req.Context())
	if err != nil {
		ar.sendResponseFromError(w, err)
		return
	}
	ar.sendNoContentEmptyResponse(w)
}

func (ar *Router) healthcheck(w http.ResponseWriter, req *http.Request) {
	if ar.monitoringService.IsHealthy(req.Context()) {
		ar.sendNoContentEmptyResponse(w)
	} else {
		ar.sendErrorResponse(w, http.StatusServiceUnavailable, common.ErrCodeServiceUnhealthy)
	}
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
	var fe common.ForqError
	if errors.As(err, &fe) {
		ar.sendErrorResponse(w, httpStatusForErrorCode(fe.Code), fe.Code)
	} else {
		ar.sendErrorResponse(w, http.StatusInternalServerError, common.ErrCodeInternal)
	}
}

func httpStatusForErrorCode(errCode string) int {
	switch {
	case strings.HasPrefix(errCode, "bad_request."):
		return http.StatusBadRequest
	case strings.HasPrefix(errCode, "not_found."):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
