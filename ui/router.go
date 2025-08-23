package ui

import (
	"fmt"
	"forq/services"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type Router struct {
	messagesService *services.MessagesService
	sessionsService *services.SessionsService
	authSecret      string
}

func NewRouter(messagesService *services.MessagesService, sessionsService *services.SessionsService, authSecret string) *Router {
	return &Router{
		messagesService: messagesService,
		sessionsService: sessionsService,
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
func (ur *Router) loginPage(w http.ResponseWriter, req *http.Request) {
	data := TemplateData{
		Title: "Login",
	}
	RenderTemplate(w, "login.html", data)
}

func (ur *Router) processLogin(w http.ResponseWriter, req *http.Request) {
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

func (ur *Router) dashboard(w http.ResponseWriter, req *http.Request) {
	data := TemplateData{
		Title: "Dashboard",
		Queues: []QueueStats{
			{Name: "emails", TotalMessages: 842, Ready: 842, Processing: 0, Failed: 0},
			{Name: "emails-dlq", TotalMessages: 2, Ready: 2, Processing: 0, Failed: 2},
			{Name: "notifications", TotalMessages: 325, Ready: 325, Processing: 0, Failed: 0},
			{Name: "notifications-dlq", TotalMessages: 1, Ready: 1, Processing: 0, Failed: 1},
			{Name: "reports", TotalMessages: 80, Ready: 80, Processing: 0, Failed: 0},
			{Name: "reports-dlq", TotalMessages: 2, Ready: 2, Processing: 0, Failed: 2},
		},
		TotalQueues:   6,
		TotalMessages: 1252,
		FailedDLQ:     5,
	}
	RenderTemplate(w, "dashboard-base.html", data)
}

func (ur *Router) queueDetails(w http.ResponseWriter, req *http.Request) {
	queueName := chi.URLParam(req, "queue")

	// Get pagination parameters
	page := 1
	isLastPage := false
	if pageStr := req.URL.Query().Get("page"); pageStr != "" {
		if pageStr == "last" {
			isLastPage = true
		} else if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// Determine if this is a DLQ queue
	isDLQ := strings.HasSuffix(queueName, "-dlq")
	queueType := "Regular"
	if isDLQ {
		queueType = "DLQ"
	}

	// Create dummy data based on queue type
	var messages []MessageDetails
	var queueDetails *QueueDetails

	if isDLQ {
		// DLQ queue with failed messages
		messages = []MessageDetails{
			{
				ID:            "01234567-89ab-cdef-0123-456789abcdef",
				Content:       `{"email": "user@example.com", "subject": "Welcome!", "body": "Welcome to our service!"}`,
				Status:        "failed",
				Attempts:      3,
				ReceivedAt:    "2024-01-15 14:30:25",
				Age:           "2 hours ago",
				FailureReason: "SMTP server timeout after 30 seconds",
			},
			{
				ID:            "fedcba98-7654-3210-fedc-ba9876543210",
				Content:       `{"email": "admin@company.com", "subject": "System Alert", "body": "Database connection failed"}`,
				Status:        "failed",
				Attempts:      5,
				ReceivedAt:    "2024-01-15 12:15:10",
				Age:           "4 hours ago",
				FailureReason: "Invalid email address format",
			},
		}

		queueDetails = &QueueDetails{
			Name:          queueName,
			Type:          queueType,
			IsDLQ:         true,
			TotalMessages: len(messages),
		}
	} else {
		// Regular queue with mixed status messages - create more for pagination testing
		messages = []MessageDetails{}

		// Generate 75 dummy messages to test pagination
		statuses := []string{"ready", "processing", "ready"}
		for i := 0; i < 75; i++ {
			messages = append(messages, MessageDetails{
				ID:         fmt.Sprintf("msg-%04d-2222-3333-4444-555555555555", i+1),
				Content:    fmt.Sprintf(`{"email": "user%d@example.com", "subject": "Message %d", "body": "This is test message number %d"}`, i+1, i+1, i+1),
				Status:     statuses[i%len(statuses)],
				Attempts:   i % 3,
				ReceivedAt: fmt.Sprintf("2024-01-15 %02d:%02d:00", 16-(i/10), 30-(i%60)),
				Age:        fmt.Sprintf("%d minutes ago", i+1),
			})
		}

		queueDetails = &QueueDetails{
			Name:          queueName,
			Type:          queueType,
			IsDLQ:         false,
			TotalMessages: len(messages),
		}
	}

	// Pagination logic - efficient approach without COUNT
	const messagesPerPage = 50
	var paginatedMessages []MessageDetails
	var hasNextPage bool
	var currentPage int

	if isLastPage {
		// For last page, get the last 50 messages (they're already in reverse chronological order)
		totalMessages := len(messages)
		start := totalMessages - messagesPerPage
		if start < 0 {
			start = 0
		}
		paginatedMessages = messages[start:]
		hasNextPage = false
		// Calculate what page this would be
		currentPage = int(math.Ceil(float64(totalMessages) / float64(messagesPerPage)))
		if currentPage < 1 {
			currentPage = 1
		}
	} else {
		// Regular pagination - get messagesPerPage + 1 to check if more exist
		start := (page - 1) * messagesPerPage
		end := start + messagesPerPage + 1 // Get one extra to check if more exist

		if start < len(messages) {
			if end > len(messages) {
				end = len(messages)
			}

			fetchedMessages := messages[start:end]

			// Check if we have more messages
			if len(fetchedMessages) > messagesPerPage {
				hasNextPage = true
				paginatedMessages = fetchedMessages[:messagesPerPage] // Remove the extra message
			} else {
				hasNextPage = false
				paginatedMessages = fetchedMessages
			}
		}
		currentPage = page
	}

	data := TemplateData{
		Title:       queueName + " - Queue Details",
		Queue:       queueDetails,
		Messages:    paginatedMessages,
		CurrentPage: currentPage,
		HasNextPage: hasNextPage,
		IsLastPage:  isLastPage,
	}

	RenderTemplate(w, "queue-base.html", data)
}

func (ur *Router) queueMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Render message browser (HTMX component)
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *Router) deleteAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete all messages from queue
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *Router) requeueAllMessages(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue all DLQ messages
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *Router) deleteMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Delete specific message
	w.WriteHeader(http.StatusNotImplemented)
}

func (ur *Router) requeueMessage(w http.ResponseWriter, req *http.Request) {
	// TODO: Requeue specific DLQ message
	w.WriteHeader(http.StatusNotImplemented)
}
