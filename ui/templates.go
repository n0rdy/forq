package ui

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

var templates *template.Template

func init() {
	var err error

	// Create template with helper functions
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}

	templates = template.New("").Funcs(funcMap)
	templates, err = templates.ParseGlob(filepath.Join("ui", "templates", "*.html"))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse templates")
	}
}

// TemplateData represents common data passed to templates
type TemplateData struct {
	Title string
	Error string
	// Dashboard specific data
	Queues        []QueueStats
	TotalQueues   int
	TotalMessages int
	Processing    int
	FailedDLQ     int
	// Queue details specific data
	Queue       *QueueDetails
	Messages    []MessageDetails
	CurrentPage int
	HasNextPage bool
	IsLastPage  bool
}

type QueueStats struct {
	Name          string
	TotalMessages int
	Ready         int
	Processing    int
	Failed        int
}

type QueueDetails struct {
	Name          string
	Type          string // "Regular" or "DLQ"
	IsDLQ         bool
	TotalMessages int
	OldestMessage *MessageSummary
	NewestMessage *MessageSummary
}

type MessageSummary struct {
	ID         string
	ReceivedAt string
	Age        string
}

type MessageDetails struct {
	ID            string
	Content       string
	Status        string
	Attempts      int
	ReceivedAt    string
	Age           string
	ProcessAfter  string
	FailureReason string
}

// RenderTemplate renders a template with the given data
func RenderTemplate(w http.ResponseWriter, templateName string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := templates.ExecuteTemplate(w, templateName, data)
	if err != nil {
		log.Error().Err(err).Str("template", templateName).Msg("Failed to render template")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
