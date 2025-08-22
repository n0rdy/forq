package api

import (
	"encoding/json"
	"errors"
	"forq/common"
	"forq/services"
	"forq/ui"
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
		// Unprotected login routes
		r.Get("/login", fr.loginPage)
		r.Post("/login", fr.processLogin)

		// Protected routes - apply middleware to specific routes
		r.With(sessionAuth(fr.sessionsService)).Get("/", fr.dashboard)
		r.Route("/queue/{queue}", func(r chi.Router) {
			r.Use(sessionAuth(fr.sessionsService))
			r.Get("/", fr.queueDetails)
			r.Get("/messages", fr.queueMessages)
			r.Delete("/messages", fr.deleteAllMessages)
			r.Post("/messages/requeue", fr.requeueAllMessages)
			r.Delete("/messages/{messageId}", fr.deleteMessage)
			r.Post("/messages/requeue/{messageId}", fr.requeueMessage)
		})
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

// UI handlers
func (fr *ForqRouter) loginPage(w http.ResponseWriter, req *http.Request) {
	data := ui.TemplateData{
		Title: "Login",
	}
	ui.RenderTemplate(w, "login_standalone.html", data)
}

func (fr *ForqRouter) processLogin(w http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse login form")
		data := ui.TemplateData{
			Title: "Login",
			Error: "Invalid form data",
		}
		ui.RenderTemplate(w, "login_standalone.html", data)
		return
	}

	token := req.FormValue("token")
	if token != fr.authSecret {
		log.Error().Msg("Invalid login token")
		data := ui.TemplateData{
			Title: "Login",
			Error: "Invalid authentication token",
		}
		ui.RenderTemplate(w, "login_standalone.html", data)
		return
	}

	// Create session
	sessionId, _ := fr.sessionsService.CreateSession()

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "ForqSession",
		Value:    sessionId,
		Path:     "/",
		HttpOnly: true,
		Secure:   req.TLS != nil, // Only secure if HTTPS
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to dashboard
	w.Header().Set("HX-Redirect", "/ui")
	w.WriteHeader(http.StatusOK)
}

func (fr *ForqRouter) dashboard(w http.ResponseWriter, req *http.Request) {
	// TODO: Render dashboard with queue overview
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) queueDetails(w http.ResponseWriter, req *http.Request) {
	// TODO: Render queue details page
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) queueMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Render message browser (HTMX component)
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) deleteAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete all messages from queue
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) requeueAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue all DLQ messages
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) deleteMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete specific message
	w.WriteHeader(http.StatusNotImplemented)
}

func (fr *ForqRouter) requeueMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue specific DLQ message
	w.WriteHeader(http.StatusNotImplemented)
}
