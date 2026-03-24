package dashboard

import (
	"html/template"
	"log"
	"net/http"
)

// NewDashboardServer mounts dashboard routes on the provided mux.
func NewDashboardServer(mux *http.ServeMux) {
	tmpl, err := template.ParseFS(TemplateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("dashboard: parse templates: %v", err)
	}
	h := NewHandler(tmpl)

	mux.HandleFunc("GET /dashboard/login", h.Login)
	mux.HandleFunc("GET /dashboard/signup", h.Login)
	mux.HandleFunc("GET /dashboard/projects", h.Projects)
	mux.HandleFunc("GET /dashboard/projects/{id}", h.Project)
	mux.HandleFunc("GET /dashboard/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/projects", http.StatusFound)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard/projects", http.StatusFound)
		}
	})
}
