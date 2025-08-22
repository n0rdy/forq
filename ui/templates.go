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
	templates, err = template.ParseGlob(filepath.Join("ui", "templates", "*.html"))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse templates")
	}
}

// TemplateData represents common data passed to templates
type TemplateData struct {
	Title string
	Error string
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
