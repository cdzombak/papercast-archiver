package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type mockAPI struct {
	archived []int64
	deleted  []int64
	err      error
}

func (m *mockAPI) Archive(_ context.Context, id int64) error {
	m.archived = append(m.archived, id)
	return m.err
}

func (m *mockAPI) Delete(_ context.Context, id int64) error {
	m.deleted = append(m.deleted, id)
	return m.err
}

func testServer(api *mockAPI) http.Handler {
	return testServerPrefix(api, "")
}

func testServerPrefix(api *mockAPI, prefix string) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(api, prefix, logger).Handler()
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func postForm(t *testing.T, h http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStatus(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/_status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"ok": true}` {
		t.Errorf("body = %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestNotFound(t *testing.T) {
	h := testServer(&mockAPI{})
	for _, target := range []string{
		"/",
		"/abc",
		"/12x34",
		"/-5",
		"/+5",
		// Numeric but too large to be a bookmark ID: 404s on every route,
		// so the choice and confirmation pages can't offer an action the
		// POST would reject.
		"/99999999999999999999999",
		"/99999999999999999999999/archive",
		"/99999999999999999999999/archive/done",
		"/123/rename",
		"/123/archive/done/extra",
		"/_status/x",
	} {
		if rec := get(t, h, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", target, rec.Code)
		}
	}
	rec := postForm(t, testServer(&mockAPI{}), "/123/rename", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /123/rename: status = %d, want 404", rec.Code)
	}
}

func TestChoosePage(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/12345?title=A+Great+Read&domain=example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"A Great Read",
		"example.com",
		`href="/12345/archive?domain=example.com&amp;title=A&#43;Great&#43;Read"`,
		`href="/12345/delete?domain=example.com&amp;title=A&#43;Great&#43;Read"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestChoosePageEscapesParams(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/1?title="+url.QueryEscape(`<script>alert("hi")</script>`))
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Error("title was not escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("escaped title not rendered")
	}
}

func TestChoosePageTruncatesParams(t *testing.T) {
	long := strings.Repeat("ü", 200)
	rec := get(t, testServer(&mockAPI{}), "/1?title="+url.QueryEscape(long))
	body := rec.Body.String()
	if strings.Contains(body, long) {
		t.Error("title was not truncated")
	}
	if !strings.Contains(body, strings.Repeat("ü", 128)+"…") {
		t.Error("truncated title with ellipsis not rendered")
	}
}

func TestChoosePageOmitsMissingParams(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/1")
	body := rec.Body.String()
	if strings.Contains(body, `class="title"`) || strings.Contains(body, `class="domain"`) {
		t.Error("empty title/domain should be omitted")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestConfirmPage(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/42/delete?title=T&domain=d.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`action="/42/delete"`,
		`method="post"`,
		`name="title" value="T"`,
		`name="domain" value="d.com"`,
		">Delete</button>",
		"Delete this article?",
		`class="cancel" href="/42?domain=d.com&amp;title=T"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestPostArchive(t *testing.T) {
	api := &mockAPI{}
	rec := postForm(t, testServer(api), "/12345/archive",
		url.Values{"title": {"My Title"}, "domain": {"example.com"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if len(api.archived) != 1 || api.archived[0] != 12345 {
		t.Errorf("archived = %v", api.archived)
	}
	if len(api.deleted) != 0 {
		t.Errorf("deleted = %v", api.deleted)
	}
	loc := rec.Header().Get("Location")
	want := "/12345/archive/done?domain=example.com&title=My+Title"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestPostDelete(t *testing.T) {
	api := &mockAPI{}
	rec := postForm(t, testServer(api), "/7/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if len(api.deleted) != 1 || api.deleted[0] != 7 {
		t.Errorf("deleted = %v", api.deleted)
	}
	if loc := rec.Header().Get("Location"); loc != "/7/delete/done" {
		t.Errorf("Location = %q", loc)
	}
}

func TestPostAPIError(t *testing.T) {
	api := &mockAPI{err: errors.New("instapaper API error 1241: no such bookmark")}
	rec := postForm(t, testServer(api), "/12345/archive", url.Values{"title": {"T"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "instapaper API error 1241: no such bookmark") {
		t.Error("body missing error details")
	}
	if !strings.Contains(body, "Something went wrong") {
		t.Error("body missing failure heading")
	}
}

func TestPostOverflowID(t *testing.T) {
	api := &mockAPI{}
	rec := postForm(t, testServer(api), "/99999999999999999999999/archive", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if len(api.archived) != 0 {
		t.Errorf("archived = %v", api.archived)
	}
}

func TestDonePage(t *testing.T) {
	rec := get(t, testServer(&mockAPI{}), "/12345/delete/done?title=T&domain=d.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Deleted", "T", "d.com", "<svg"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}

	rec = get(t, testServer(&mockAPI{}), "/12345/archive/done")
	if !strings.Contains(rec.Body.String(), "Archived") {
		t.Error("archive done page missing heading")
	}
}

func TestPathPrefixChoosePage(t *testing.T) {
	h := testServerPrefix(&mockAPI{}, "/secret")
	// Links carry the prefix whether or not the request path does, so the
	// app works behind both stripping and non-stripping proxies.
	for _, target := range []string{"/secret/12345?title=T", "/12345?title=T"} {
		rec := get(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", target, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`href="/secret/12345/archive?title=T"`,
			`href="/secret/12345/delete?title=T"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: body missing %q", target, want)
			}
		}
	}
}

func TestPathPrefixConfirmPage(t *testing.T) {
	rec := get(t, testServerPrefix(&mockAPI{}, "/secret"), "/secret/42/delete?title=T")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`action="/secret/42/delete"`,
		`class="cancel" href="/secret/42?title=T"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestPathPrefixPostRedirect(t *testing.T) {
	api := &mockAPI{}
	rec := postForm(t, testServerPrefix(api, "/secret"), "/secret/7/archive", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if len(api.archived) != 1 || api.archived[0] != 7 {
		t.Errorf("archived = %v", api.archived)
	}
	if loc := rec.Header().Get("Location"); loc != "/secret/7/archive/done" {
		t.Errorf("Location = %q", loc)
	}
}

func TestPathPrefixStatus(t *testing.T) {
	h := testServerPrefix(&mockAPI{}, "/secret")
	for _, target := range []string{"/_status", "/secret/_status"} {
		if rec := get(t, h, target); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d", target, rec.Code)
		}
	}
}

func TestPathPrefixBoundary(t *testing.T) {
	// "/secrets/..." must not be treated as prefixed; it falls through to
	// the mux, where "secrets" is a non-numeric id.
	rec := get(t, testServerPrefix(&mockAPI{}, "/secret"), "/secrets/1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCutPathPrefix(t *testing.T) {
	tests := []struct {
		path, prefix, want string
		ok                 bool
	}{
		{"/secret/1", "/secret", "/1", true},
		{"/secret", "/secret", "/", true},
		{"/secrets/1", "/secret", "/secrets/1", false},
		{"/1", "/secret", "/1", false},
		{"/a/b/1", "/a/b", "/1", true},
	}
	for _, tt := range tests {
		got, ok := cutPathPrefix(tt.path, tt.prefix)
		if got != tt.want || ok != tt.ok {
			t.Errorf("cutPathPrefix(%q, %q) = %q, %v; want %q, %v",
				tt.path, tt.prefix, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRejectsCrossSitePost(t *testing.T) {
	api := &mockAPI{}
	req := httptest.NewRequest(http.MethodPost, "/12345/delete", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	testServer(api).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if len(api.deleted) != 0 {
		t.Errorf("deleted = %v, want no API call", api.deleted)
	}
}

func TestAllowsSameSitePost(t *testing.T) {
	// "none" is a user-initiated navigation; an absent header is a client
	// that doesn't send fetch metadata. Neither is blocked.
	for _, site := range []string{"same-origin", "same-site", "none", ""} {
		api := &mockAPI{}
		req := httptest.NewRequest(http.MethodPost, "/12345/archive", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		testServer(api).ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("Sec-Fetch-Site %q: status = %d, want 303", site, rec.Code)
		}
		if len(api.archived) != 1 {
			t.Errorf("Sec-Fetch-Site %q: archived = %v", site, api.archived)
		}
	}
}

func TestAllowsCrossSiteGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/12345", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	testServer(&mockAPI{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestParseBookmarkID(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1", 1, false},
		{"12345", 12345, false},
		{"042", 42, false},
		{"9223372036854775807", 9223372036854775807, false},
		{"9223372036854775808", 0, true},
		{"", 0, true},
		{"-5", 0, true},
		{"+5", 0, true},
		{"12x34", 0, true},
	}
	for _, tt := range tests {
		got, err := parseBookmarkID(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseBookmarkID(%q): want error, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBookmarkID(%q): %v", tt.in, err)
		} else if got != tt.want {
			t.Errorf("parseBookmarkID(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcde…"},
		{"éééééé", 5, "ééééé…"},
	}
	for _, tt := range tests {
		if got := truncateRunes(tt.in, tt.max); got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}
