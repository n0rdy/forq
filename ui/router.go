package ui

import (
	"forq/common"
	"forq/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type Router struct {
	messagesService *services.MessagesService
	sessionsService *services.SessionsService
	queuesService   *services.QueuesService
	authSecret      string
}

func NewRouter(messagesService *services.MessagesService, sessionsService *services.SessionsService, queuesService *services.QueuesService, authSecret string) *Router {
	return &Router{
		messagesService: messagesService,
		sessionsService: sessionsService,
		queuesService:   queuesService,
		authSecret:      authSecret,
	}
}

func (ur *Router) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Route("/ui", func(r chi.Router) {
		// Unprotected login routes
		r.Get("/login", ur.loginPage)
		r.Post("/login", ur.processLogin)
		r.Post("/logout", ur.processLogout)

		// Protected routes - apply middleware to specific routes
		r.With(sessionAuth(ur.sessionsService)).
			Get("/", ur.dashboardPage)

		r.Route("/queue/{queue}", func(r chi.Router) {
			r.Use(sessionAuth(ur.sessionsService))

			r.Get("/", ur.queueDetailsPage)
			r.Get("/messages", ur.queueMessages)
			r.Get("/messages/{messageId}/details", ur.messageDetails)
			r.Delete("/messages", ur.deleteAllMessages)
			r.Post("/messages/requeue", ur.requeueAllMessages)
			r.Delete("/messages/{messageId}", ur.deleteMessage)
			r.Post("/messages/requeue/{messageId}", ur.requeueMessage)
		})
	})

	return router
}

// UI handlers
func (ur *Router) loginPage(w http.ResponseWriter, req *http.Request) {
	data := common.LoginPageData{
		Title: "Login",
	}
	RenderTemplate(w, "login.html", data)
}

func (ur *Router) processLogin(w http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse login form")
		data := common.LoginPageData{
			Title: "Login",
			Error: "Invalid form data",
		}
		RenderTemplate(w, "login.html", data)
		return
	}

	token := req.FormValue("token")
	if token != ur.authSecret {
		log.Error().Msg("Invalid login token")
		data := common.LoginPageData{
			Title: "Login",
			Error: "Invalid authentication token",
		}
		RenderTemplate(w, "login.html", data)
		return
	}

	// Create session
	sessionId, _ := ur.sessionsService.CreateSession()

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

func (ur *Router) processLogout(w http.ResponseWriter, req *http.Request) {
	sessionCookie, _ := req.Cookie("ForqSession")
	if sessionCookie != nil {
		ur.sessionsService.InvalidateSession(sessionCookie.Value)
	}

	// Clear the session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "ForqSession",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Delete the cookie
		HttpOnly: true,
	})

	// Redirect to login page
	http.Redirect(w, req, "/ui/login", http.StatusFound)
}

func (ur *Router) dashboardPage(w http.ResponseWriter, req *http.Request) {
	dashboardData, err := ur.queuesService.GetQueuesStats(req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	RenderTemplate(w, "dashboard-base.html", dashboardData)
}

func (ur *Router) queueDetailsPage(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	queueStats, err := ur.queuesService.GetQueueStats(queueName, req.Context())
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Msg("failed to get queue stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if queueStats == nil {
		http.NotFound(w, req)
		return
	}

	data := common.QueuePageData{
		Title: queueName + " - Queue Details",
		Queue: queueStats,
	}

	RenderTemplate(w, "queue-base.html", data)
}

func (ur *Router) queueMessages(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")
	cursor := req.URL.Query().Get("after")

	const messagesLimit = 50

	messagesData, err := ur.messagesService.GetMessagesForUI(queueName, cursor, messagesLimit, req.Context())
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Str("cursor", cursor).Msg("failed to get messages for UI")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Choose template based on whether this is initial load or infinite scroll
	template := "messages-component.html"
	if cursor != "" {
		// For infinite scroll, use append template
		template = "messages-append.html"
	}

	RenderTemplate(w, template, messagesData)
}

func (ur *Router) messageDetails(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")
	messageId := chi.URLParam(req, "messageId")

	messageDetails, err := ur.messagesService.GetMessageDetails(messageId, queueName, req.Context())
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).Str("message_id", messageId).Msg("failed to get message details")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if messageDetails == nil {
		http.NotFound(w, req)
		return
	}

	RenderTemplate(w, "message-details.html", messageDetails)
}

func (ur *Router) deleteAllMessages(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.DeleteAllDlqMessages(queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Redirect to dashboard, as most likely the queue is now gone
	// TODO: consider passing a message via query param to show that the operation was successful
	w.Header().Set("HX-Redirect", "/ui")
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) requeueAllMessages(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.RequeueAllDlqMessages(queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Redirect to dashboard, as most likely the DLQ is now empty
	// TODO: consider passing a message via query param to show that the operation was successful
	w.Header().Set("HX-Redirect", "/ui")
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) deleteMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.DeleteDlqMessage(messageId, queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Respond with 200 OK to indicate success
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) requeueMessage(w http.ResponseWriter, req *http.Request) {
	messageId := chi.URLParam(req, "messageId")
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.RequeueDlqMessage(messageId, queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Respond with 200 OK to indicate success
	w.WriteHeader(http.StatusOK)
}
