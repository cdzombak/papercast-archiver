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
	"strings"
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
	api    instapaper.API
	prefix string // base URL path prefix: "" or "/like/this"
	log    *slog.Logger
	pages  map[string]*template.Template
}

// New creates a Server. pathPrefix is the base URL's path component; it must
// be "" or start with "/" and not end with one (see config.ServerConfig.PathPrefix).
func New(api instapaper.API, pathPrefix string, log *slog.Logger) *Server {
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		pages[name] = template.Must(template.ParseFS(templateFS, "templates/base.html", "templates/"+name))
	}
	return &Server{api: api, prefix: pathPrefix, log: log, pages: pages}
}

// Handler returns the root HTTP handler, with request logging attached.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_status", s.handleStatus)
	mux.HandleFunc("GET /{id}", s.handleChoose)
	mux.HandleFunc("GET /{id}/{action}", s.handleConfirm)
	mux.HandleFunc("POST /{id}/{action}", s.handleAction)
	mux.HandleFunc("GET /{id}/{action}/done", s.handleDone)
	var h http.Handler = mux
	if s.prefix != "" {
		h = stripPrefix(s.prefix, mux)
	}
	return s.logRequests(h)
}

// stripPrefix serves requests whose path begins with prefix by removing it,
// and passes all other requests through unchanged. This makes the app work
// whether or not a reverse proxy strips the prefix, and when it is reached
// directly at host:port.
func stripPrefix(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := cutPathPrefix(r.URL.Path, prefix)
		rp, rok := cutPathPrefix(r.URL.RawPath, prefix)
		if !ok || (r.URL.RawPath != "" && !rok) {
			next.ServeHTTP(w, r)
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = p
		r2.URL.RawPath = rp
		next.ServeHTTP(w, r2)
	})
}

// cutPathPrefix removes prefix from path at a "/" boundary, so that
// prefix "/secret" matches "/secret" and "/secret/1" but not "/secrets".
// It reports ok=false, returning path unchanged, when there is no match.
func cutPathPrefix(path, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok || (rest != "" && rest[0] != '/') {
		return path, false
	}
	if rest == "" {
		rest = "/"
	}
	return rest, true
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

// reqParams holds the validated inputs to a request handler.
type reqParams struct {
	ID         string // bookmark ID exactly as it appeared in the path
	BookmarkID int64
	Action     string // "archive" or "delete"; empty when not part of the route
	Title      string
	Domain     string
}

// requestParams extracts and validates the id and optional action path
// segments, and the truncated title/domain params (from the query on GET,
// the form body on POST). It reports ok=false after replying 404.
func (s *Server) requestParams(w http.ResponseWriter, r *http.Request, needAction bool) (reqParams, bool) {
	p := reqParams{ID: r.PathValue("id")}
	bookmarkID, err := parseBookmarkID(p.ID)
	if err != nil {
		http.NotFound(w, r)
		return reqParams{}, false
	}
	p.BookmarkID = bookmarkID
	if needAction {
		p.Action = r.PathValue("action")
		if p.Action != "archive" && p.Action != "delete" {
			http.NotFound(w, r)
			return reqParams{}, false
		}
	}
	p.Title = truncateRunes(r.FormValue("title"), maxParamRunes)
	p.Domain = truncateRunes(r.FormValue("domain"), maxParamRunes)
	return p, true
}

func (s *Server) handleChoose(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requestParams(w, r, false)
	if !ok {
		return
	}
	q := paramQuery(p.Title, p.Domain)
	s.render(w, http.StatusOK, "choose.html", pageData{
		Title:     p.Title,
		Domain:    p.Domain,
		ID:        p.ID,
		ActionURL: s.prefix + "/" + p.ID + "/archive" + q,
		AltAction: s.prefix + "/" + p.ID + "/delete" + q,
		HeadTitle: "Archive or delete?",
		Heading:   "Archive or delete?",
	})
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}
	label := actionLabel(p.Action)
	s.render(w, http.StatusOK, "confirm.html", pageData{
		Title:      p.Title,
		Domain:     p.Domain,
		ID:         p.ID,
		Action:     p.Action,
		ActionURL:  s.prefix + "/" + p.ID + "/" + p.Action,
		CancelURL:  s.prefix + "/" + p.ID + paramQuery(p.Title, p.Domain),
		HeadTitle:  "Confirm " + p.Action,
		Heading:    label + " this article?",
		ButtonText: label,
	})
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}

	var err error
	if p.Action == "archive" {
		err = s.api.Archive(r.Context(), p.BookmarkID)
	} else {
		err = s.api.Delete(r.Context(), p.BookmarkID)
	}
	if err != nil {
		s.log.Error("instapaper API call failed", "action", p.Action, "id", p.ID, "error", err)
		s.render(w, http.StatusInternalServerError, "error.html", pageData{
			Title:     p.Title,
			Domain:    p.Domain,
			ID:        p.ID,
			Action:    p.Action,
			ErrorText: err.Error(),
			HeadTitle: "Error",
			Heading:   "Something went wrong",
		})
		return
	}

	s.log.Info("article state changed", "action", p.Action, "id", p.ID, "title", p.Title)
	http.Redirect(w, r, s.prefix+"/"+p.ID+"/"+p.Action+"/done"+paramQuery(p.Title, p.Domain), http.StatusSeeOther)
}

func (s *Server) handleDone(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requestParams(w, r, true)
	if !ok {
		return
	}
	heading := "Archived"
	if p.Action == "delete" {
		heading = "Deleted"
	}
	s.render(w, http.StatusOK, "done.html", pageData{
		Title:     p.Title,
		Domain:    p.Domain,
		ID:        p.ID,
		Action:    p.Action,
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

// parseBookmarkID parses a path segment into an Instapaper bookmark ID.
// A sign, any non-digit, and a value too large for an int64 are all rejected,
// so every route agrees on which IDs exist.
func parseBookmarkID(s string) (int64, error) {
	v, err := strconv.ParseUint(s, 10, 63)
	return int64(v), err
}

// truncateRunes limits s to max runes, appending an ellipsis if truncated.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
