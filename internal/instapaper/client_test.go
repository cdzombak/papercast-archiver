package instapaper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient("ckey", "csecret", &Credentials{Token: "tok", TokenSecret: "sec"},
		WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
}

func TestRequestAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/access_token" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("x_auth_mode"); got != "client_auth" {
			t.Errorf("x_auth_mode = %q", got)
		}
		if got := r.PostForm.Get("x_auth_username"); got != "user@example.com" {
			t.Errorf("x_auth_username = %q", got)
		}
		if auth := r.Header.Get("Authorization"); !strings.Contains(auth, `oauth_consumer_key="ckey"`) {
			t.Errorf("Authorization header missing consumer key: %q", auth)
		}
		_, _ = w.Write([]byte("oauth_token=newtok&oauth_token_secret=newsec"))
	}))
	defer srv.Close()

	c := NewClient("ckey", "csecret", nil, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	creds, err := c.RequestAccessToken(context.Background(), "user@example.com", "pw")
	if err != nil {
		t.Fatalf("RequestAccessToken: %v", err)
	}
	if creds.Token != "newtok" || creds.TokenSecret != "newsec" {
		t.Errorf("creds = %+v", creds)
	}
}

func TestRequestAccessTokenFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Invalid credentials.", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient("ckey", "csecret", nil, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := c.RequestAccessToken(context.Background(), "user@example.com", "wrong")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid credentials.") {
		t.Errorf("error %q missing status/body", err)
	}
}

func TestArchive(t *testing.T) {
	var gotPath, gotID string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotID = r.PostForm.Get("bookmark_id")
		if auth := r.Header.Get("Authorization"); !strings.Contains(auth, `oauth_token="tok"`) {
			t.Errorf("Authorization header missing token: %q", auth)
		}
		_, _ = w.Write([]byte(`[{"type":"bookmark","bookmark_id":12345}]`))
	})

	if err := c.Archive(context.Background(), 12345); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if gotPath != "/bookmarks/archive" {
		t.Errorf("path = %q", gotPath)
	}
	if gotID != "12345" {
		t.Errorf("bookmark_id = %q", gotID)
	}
}

func TestDelete(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[]`))
	})

	if err := c.Delete(context.Background(), 99); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotPath != "/bookmarks/delete" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestBookmarkActionAPIError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"type":"error","error_code":1241,"message":"Invalid or missing bookmark_id"}]`))
	})

	err := c.Archive(context.Background(), 12345)
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 1241 {
		t.Errorf("Code = %d", apiErr.Code)
	}
}

func TestBookmarkActionHTTPError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server exploded", http.StatusInternalServerError)
	})

	err := c.Delete(context.Background(), 5)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "server exploded") {
		t.Errorf("error %q missing status/body", err)
	}
}

func TestActionWithoutToken(t *testing.T) {
	c := NewClient("ckey", "csecret", nil)
	if err := c.Archive(context.Background(), 1); err == nil {
		t.Fatal("want error when unauthenticated")
	}
}
