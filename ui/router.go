package ui

import (
	"crypto/subtle"
	"net/http"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/services"
	"github.com/n0rdy/forq/utils"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type Router struct {
	messagesService   *services.MessagesService
	sessionsService   *services.SessionsService
	queuesService     *services.QueuesService
	throttlingService *services.ThrottlingService
	authSecret        string
	env               string
	trustProxyHeaders bool
}

func NewRouter(messagesService *services.MessagesService, sessionsService *services.SessionsService, queuesService *services.QueuesService, throttlingService *services.ThrottlingService, authSecret string, env string, trustProxyHeaders bool) *Router {
	return &Router{
		messagesService:   messagesService,
		sessionsService:   sessionsService,
		queuesService:     queuesService,
		throttlingService: throttlingService,
		authSecret:        authSecret,
		env:               env,
		trustProxyHeaders: trustProxyHeaders,
	}
}

func (ur *Router) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Use(securityHeaders(ur.env))
	router.Use(bodyLimit(maxUIBodyBytes))
	router.Use(csrfPrevention(ur.csrfErrorHandler, ur.env))

	// embedded static assets (CSS, HTMX, theme script) - unauthenticated, as
	// the login page needs them too. Cacheable (overriding the no-store set by
	// securityHeaders): the assets only change on releases, and 1 hour of
	// staleness after an upgrade is acceptable.
	staticHandler := http.FileServerFS(staticFS)
	router.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		staticHandler.ServeHTTP(w, req)
	}))

	// unprotected login routes (failed attempts are throttled in processLogin):
	router.Get("/login", ur.loginPage)
	router.Post("/login", ur.processLogin)

	// protected routes:
	router.With(sessionAuth(ur.sessionsService)).
		Get("/", ur.dashboardPage)

	router.With(sessionAuth(ur.sessionsService)).Post("/logout", ur.processLogout)

	router.Route("/queue/{queue}", func(r chi.Router) {
		r.Use(sessionAuth(ur.sessionsService)) // session auth for all queue routes
		r.Use(validateQueueName)

		r.Get("/", ur.queueDetailsPage)
		r.Get("/messages", ur.queueMessages)
		r.Get("/messages/{messageId}/details", ur.messageDetails)
		r.Delete("/messages", ur.deleteAllMessages)
		r.Post("/messages/requeue", ur.requeueAllMessages)
		r.Delete("/messages/{messageId}", ur.deleteMessage)
		r.Post("/messages/requeue/{messageId}", ur.requeueMessage)
	})

	return router
}

func (ur *Router) loginPage(w http.ResponseWriter, req *http.Request) {
	data := common.LoginPageData{
		Title: "Login",
	}

	RenderTemplate(w, req, "login.html", data)
}

func (ur *Router) processLogin(w http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse login form")
		data := common.LoginPageData{
			Title: "Login",
			Error: "Invalid form data",
		}
		RenderTemplate(w, req, "login.html", data)
		return
	}

	// the token is checked FIRST (constant-time) so that a valid token always
	// works even while the IP is locked out - behind a proxy without
	// FORQ_TRUST_PROXY_HEADERS all clients share the proxy's IP, and someone
	// else's failed attempts must not lock the admin out.
	token := req.FormValue("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(ur.authSecret)) != 1 {
		ip := utils.ClientIP(req, ur.trustProxyHeaders)
		if ur.throttlingService.IsLocked(ip) {
			data := common.LoginPageData{
				Title: "Login",
				Error: "Too many failed login attempts. Try again in a minute.",
			}
			RenderTemplateWithStatus(w, req, http.StatusTooManyRequests, "login.html", data)
			return
		}
		ur.throttlingService.RecordFailure(ip)
		log.Error().Msg("Invalid login token")
		data := common.LoginPageData{
			Title: "Login",
			Error: "Invalid authentication token",
		}
		RenderTemplateWithStatus(w, req, http.StatusUnauthorized, "login.html", data)
		return
	}

	sessionId, _ := ur.sessionsService.CreateSession()

	http.SetCookie(w, &http.Cookie{
		Name:     "ForqSession",
		Value:    sessionId,
		Path:     "/",
		HttpOnly: true,
		Secure:   ur.env == common.ProEnv,
		SameSite: http.SameSiteLaxMode,
	})

	// redirects to dashboard on successful login
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) processLogout(w http.ResponseWriter, req *http.Request) {
	sessionCookie, _ := req.Cookie("ForqSession")
	if sessionCookie != nil {
		ur.sessionsService.InvalidateSession(sessionCookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "ForqSession",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // delete the cookie
		HttpOnly: true,
		Secure:   ur.env == common.ProEnv,
		SameSite: http.SameSiteLaxMode,
	})

	// redirects to login page
	http.Redirect(w, req, "/login", http.StatusFound)
}

func (ur *Router) dashboardPage(w http.ResponseWriter, req *http.Request) {
	dashboardData, err := ur.queuesService.GetQueuesStats(req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	RenderTemplate(w, req, "dashboard-base.html", dashboardData)
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

	RenderTemplate(w, req, "queue-base.html", data)
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

	// chooses template based on whether this is initial load or infinite scroll
	template := "messages-component.html"
	if cursor != "" {
		// for infinite scroll, uses append template
		template = "messages-append.html"
	}

	RenderTemplate(w, req, template, messagesData)
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

	RenderTemplate(w, req, "message-details.html", messageDetails)
}

func (ur *Router) deleteAllMessages(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.DeleteAllDlqMessages(queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// redirects to dashboard, as most likely the queue is now gone
	// TODO: consider passing a message via query param to show that the operation was successful
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) requeueAllMessages(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	err := ur.messagesService.RequeueAllDlqMessages(queueName, req.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// redirects to dashboard, as most likely the DLQ is now empty
	// TODO: consider passing a message via query param to show that the operation was successful
	w.Header().Set("HX-Redirect", "/")
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
	w.WriteHeader(http.StatusOK)
}

func (ur *Router) csrfErrorHandler(w http.ResponseWriter, r *http.Request) {
	log.Error().
		Str("path", r.URL.Path).
		Str("method", r.Method).
		Msg("CSRF validation failed")

	// For HTMX requests, return appropriate error
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Retarget", "body")
		w.Header().Set("HX-Reswap", "innerHTML")
		http.Error(w, "Security validation failed. Please refresh the page and try again.", http.StatusForbidden)
		return
	}

	// For regular requests, redirect to login page
	http.Redirect(w, r, "/login", http.StatusFound)
}
