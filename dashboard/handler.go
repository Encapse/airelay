package dashboard

import (
	"html/template"
	"net/http"
)

// Handler serves dashboard HTML pages.
type Handler struct {
	tmpl *template.Template
}

func NewHandler(tmpl *template.Template) *Handler {
	return &Handler{tmpl: tmpl}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "projects.html", nil)
}

func (h *Handler) Project(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "project.html", nil)
}
