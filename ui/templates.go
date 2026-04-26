package ui

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/justinas/nosurf"
	"github.com/rs/zerolog/log"
)

var templates *template.Template

func init() {
	var err error

	// Create template with helper functions
	funcMap := template.FuncMap{
		"add":     func(a, b int) int { return a + b },
		"sub":     func(a, b int) int { return a - b },
		"toUpper": strings.ToUpper,
	}

	templates = template.New("").Funcs(funcMap)
	templates, err = templates.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse templates")
	}
}

// RenderTemplate renders a template with the given data and CSRF token, status 200.
func RenderTemplate(w http.ResponseWriter, req *http.Request, templateName string, data interface{}) {
	RenderTemplateWithStatus(w, req, http.StatusOK, templateName, data)
}

// RenderTemplateWithStatus renders into a buffer first so a render failure can
// still surface as a 500 response with no partial body written.
func RenderTemplateWithStatus(w http.ResponseWriter, req *http.Request, status int, templateName string, data interface{}) {
	templateData := map[string]interface{}{
		"Data":      data,
		"CSRFToken": nosurf.Token(req),
	}

	if dataMap, ok := data.(map[string]interface{}); ok {
		for key, value := range dataMap {
			templateData[key] = value
		}
	}

	var buf bytes.Buffer
	err := templates.ExecuteTemplate(&buf, templateName, templateData)
	if err != nil {
		log.Error().Err(err).Str("template", templateName).Msg("Failed to render template")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}
