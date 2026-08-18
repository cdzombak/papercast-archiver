// Package server implements the papercast-archiver web UI: a per-article
// page offering archive/delete, a confirmation page, and a success page.
package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cdzombak/papercast-archiver/internal/instapaper"
)

//go:embed templates/*.html
var templateFS embed.FS

// maxParamRunes is the display limit for the title and domain query params.
const maxParamRunes = 128

var pageNames = []string{"choose.html", "confirm.html", "done.html", "error.html"}

// Server handles HTTP requests. Construct with New.
type Server struct {
	api     instapaper.API
	baseURL string // no trailing slash
	log     *slog.Logger
	pages   map[string]*template.Template
}

// New creates a Server. baseURL must not end with a trailing slash.
func New(api instapaper.API, baseURL string, log *slog.Logger) *Server {
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		pages[name] = template.Must(template.ParseFS(templateFS, "templates/base.html", "templates/"+name))
	}
	return &Server{api: api, baseURL: baseURL, log: log, pages: pages}
}

// Handler returns the root HTTP handler, with request logging attached.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_status", s.handleStatus)
	mux.HandleFunc("GET /{id}", s.handleChoose)
	mux.HandleFunc("GET /{id}/{action}", s.handleConfirm)
	mux.HandleFunc("POST /{id}/{action}", s.handleAction)
	mux.HandleFunc("GET /{id}/{action}/done", s.handleDone)
	return s.logRequests(mux)
}

// logRequests logs every request at debug level.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Debug("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
			"remote", r.RemoteAddr)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// pageData is the data passed to every page template.
type pageData struct {
	Title      string // article title, possibly empty
	Domain     string // article source domain, possibly empty
	ID         string
	Action     string // "archive" or "delete"
	ActionURL  string // confirm-page link or POST target
	AltAction  string // the other action's confirm-page link (choose page)
	CancelURL  string // back to the decision page (confirm page)
	ErrorText  string // error page only
	HeadTitle  string // <title> content
	Heading    string
	ButtonText string
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok": true}`))
}

// requestParams extracts and validates the id and optional action path
// segments, and the truncated title/domain params (from the query on GET,
// the form body on POST). It reports ok=false after replying 404.
func (s *Server) requestParams(w http.ResponseWriter, r *http.Request, needAction bool) (id, action, title, domain string, ok bool) {
	id = r.PathValue("id")
	if !isNumeric(id) {
		http.NotFound(w, r)
		return "", "", "", "", false
	}
	if needAction {
		action = r.PathValue("action")
		if action != "archive" && action != "delete" {
			http.NotFound(w, r)
			return "", "", "", "", false
		}
	}
	title = truncateRunes(r.FormValue("title"), maxParamRunes)
	domain = truncateRunes(r.FormValue("domain"), maxParamRunes)
	return id, action, title, domain, true
}

func (s *Server) handleChoose(w http.ResponseWriter, r *http.Request) {
	id, _, title, domain, ok := s.requestParams(w, r, false)
	if !ok {
		return
	}
	q := paramQuery(title, domain)
	s.render(w, http.StatusOK, "choose.html", pageData{
		Title:     title,
		Domain:    domain,
		ID:        id,
		ActionURL: "/" + id + "/archive" + q,
		AltAction: "/" + id + "/delete" + q,
		HeadTitle: "Archive or delete?",
		Heading:   "Archive or delete?",
	})
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	id, action, title, domain, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}
	label := actionLabel(action)
	s.render(w, http.StatusOK, "confirm.html", pageData{
		Title:      title,
		Domain:     domain,
		ID:         id,
		Action:     action,
		ActionURL:  "/" + id + "/" + action,
		CancelURL:  "/" + id + paramQuery(title, domain),
		HeadTitle:  "Confirm " + action,
		Heading:    label + " this article?",
		ButtonText: label,
	})
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	id, action, title, domain, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}
	bookmarkID, err := parseBookmarkID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if action == "archive" {
		err = s.api.Archive(r.Context(), bookmarkID)
	} else {
		err = s.api.Delete(r.Context(), bookmarkID)
	}
	if err != nil {
		s.log.Error("instapaper API call failed", "action", action, "id", id, "error", err)
		s.render(w, http.StatusInternalServerError, "error.html", pageData{
			Title:     title,
			Domain:    domain,
			ID:        id,
			Action:    action,
			ErrorText: err.Error(),
			HeadTitle: "Error",
			Heading:   "Something went wrong",
		})
		return
	}

	s.log.Info("article state changed", "action", action, "id", id, "title", title)
	http.Redirect(w, r, s.baseURL+"/"+id+"/"+action+"/done"+paramQuery(title, domain), http.StatusSeeOther)
}

func (s *Server) handleDone(w http.ResponseWriter, r *http.Request) {
	id, action, title, domain, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}
	heading := "Archived"
	if action == "delete" {
		heading = "Deleted"
	}
	s.render(w, http.StatusOK, "done.html", pageData{
		Title:     title,
		Domain:    domain,
		ID:        id,
		Action:    action,
		HeadTitle: heading,
		Heading:   heading,
	})
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[page].ExecuteTemplate(w, "base.html", data); err != nil {
		s.log.Error("render template", "page", page, "error", err)
	}
}

// paramQuery builds a "?title=...&domain=..." query string, omitting empty
// values. Returns "" if both are empty.
func paramQuery(title, domain string) string {
	v := url.Values{}
	if title != "" {
		v.Set("title", title)
	}
	if domain != "" {
		v.Set("domain", domain)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func actionLabel(action string) string {
	if action == "delete" {
		return "Delete"
	}
	return "Archive"
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseBookmarkID parses a numeric path segment into an Instapaper bookmark ID.
func parseBookmarkID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// truncateRunes limits s to max runes, appending an ellipsis if truncated.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
