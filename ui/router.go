package ui

import (
	"forq/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type UIRouter struct {
	messagesService *services.MessagesService
	sessionsService *services.SessionsService
	authSecret      string
}

func NewRouter(messagesService *services.MessagesService, sessionsService *services.SessionsService, authSecret string) *UIRouter {
	return &UIRouter{
		messagesService: messagesService,
		sessionsService: sessionsService,
		authSecret:      authSecret,
	}
}

func (ur *UIRouter) NewRouter() *chi.Mux {
	router := chi.NewRouter()

	router.Route("/ui", func(r chi.Router) {
		// Unprotected login routes
		r.Get("/login", ur.loginPage)
		r.Post("/login", ur.processLogin)

		// Protected routes - apply middleware to specific routes
		r.With(sessionAuth(ur.sessionsService)).Get("/", ur.dashboard)
		r.Route("/queue/{queue}", func(r chi.Router) {
			r.Use(sessionAuth(ur.sessionsService))
			r.Get("/", ur.queueDetails)
			r.Get("/messages", ur.queueMessages)
			r.Delete("/messages", ur.deleteAllMessages)
			r.Post("/messages/requeue", ur.requeueAllMessages)
			r.Delete("/messages/{messageId}", ur.deleteMessage)
			r.Post("/messages/requeue/{messageId}", ur.requeueMessage)
		})
	})

	return router
}

// UI handlers
func (ur *UIRouter) loginPage(w http.ResponseWriter, req *http.Request) {
	data := TemplateData{
		Title: "Login",
	}
	RenderTemplate(w, "login.html", data)
}

func (ur *UIRouter) processLogin(w http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse login form")
		data := TemplateData{
			Title: "Login",
			Error: "Invalid form data",
		}
		RenderTemplate(w, "login.html", data)
		return
	}

	token := req.FormValue("token")
	if token != ur.authSecret {
		log.Error().Msg("Invalid login token")
		data := TemplateData{
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

func (ur *UIRouter) dashboard(w http.ResponseWriter, req *http.Request) {
	// TODO: Render dashboard with queue overview
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) queueDetails(w http.ResponseWriter, req *http.Request) {
	// TODO: Render queue details page
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) queueMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Render message browser (HTMX component)
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) deleteAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete all messages from queue
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) requeueAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue all DLQ messages
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) deleteMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete specific message
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *UIRouter) requeueMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue specific DLQ message
	w.WriteHeader(http.StatusNotImplemented)
}
