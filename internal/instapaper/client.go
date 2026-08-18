package instapaper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gomodule/oauth1/oauth"
)

const defaultBaseURL = "https://www.instapaper.com/api/1"

// maxErrBodyLen limits how much of an error response body is included in errors.
const maxErrBodyLen = 512

// API is the subset of the Instapaper API the web server depends on.
type API interface {
	Archive(ctx context.Context, bookmarkID int64) error
	Delete(ctx context.Context, bookmarkID int64) error
}

// Client is an Instapaper Full API client.
type Client struct {
	oauth      oauth.Client
	token      *oauth.Credentials // nil until authenticated
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (for tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient overrides the HTTP client used for requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// NewClient creates an Instapaper client. creds may be nil, in which case
// only RequestAccessToken may be called.
func NewClient(consumerKey, consumerSecret string, creds *Credentials, opts ...Option) *Client {
	c := &Client{
		oauth: oauth.Client{
			Credentials: oauth.Credentials{Token: consumerKey, Secret: consumerSecret},
		},
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
	}
	if creds != nil {
		c.token = &oauth.Credentials{Token: creds.Token, Secret: creds.TokenSecret}
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError is an error object returned by the Instapaper API.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("instapaper API error %d: %s", e.Code, e.Message)
}

// RequestAccessToken exchanges a username/password for a permanent OAuth
// token pair using Instapaper's xAuth flow.
func (c *Client) RequestAccessToken(ctx context.Context, username, password string) (*Credentials, error) {
	form := url.Values{
		"x_auth_username": {username},
		"x_auth_password": {password},
		"x_auth_mode":     {"client_auth"},
	}
	body, err := c.post(ctx, &oauth.Credentials{}, "/oauth/access_token", form)
	if err != nil {
		return nil, fmt.Errorf("instapaper access token request: %w", err)
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("instapaper access token request: parse response: %w", err)
	}
	creds := &Credentials{
		Token:       vals.Get("oauth_token"),
		TokenSecret: vals.Get("oauth_token_secret"),
	}
	if creds.Token == "" || creds.TokenSecret == "" {
		return nil, fmt.Errorf("instapaper access token request: response missing token pair: %q", truncate(body))
	}
	return creds, nil
}

// Archive moves a bookmark to the archive via bookmarks/archive.
func (c *Client) Archive(ctx context.Context, bookmarkID int64) error {
	return c.bookmarkAction(ctx, "archive", bookmarkID)
}

// Delete permanently deletes a bookmark via bookmarks/delete.
func (c *Client) Delete(ctx context.Context, bookmarkID int64) error {
	return c.bookmarkAction(ctx, "delete", bookmarkID)
}

func (c *Client) bookmarkAction(ctx context.Context, action string, bookmarkID int64) error {
	form := url.Values{"bookmark_id": {strconv.FormatInt(bookmarkID, 10)}}
	body, err := c.authedPost(ctx, "/bookmarks/"+action, form)
	if err != nil {
		return fmt.Errorf("instapaper bookmarks/%s (bookmark %d): %w", action, bookmarkID, err)
	}
	if err := checkAPIError(body); err != nil {
		return fmt.Errorf("instapaper bookmarks/%s (bookmark %d): %w", action, bookmarkID, err)
	}
	return nil
}

func (c *Client) authedPost(ctx context.Context, path string, form url.Values) ([]byte, error) {
	if c.token == nil {
		return nil, errors.New(`no access token (run "papercast-archiver instapaper-login" first)`)
	}
	return c.post(ctx, c.token, path, form)
}

// post signs and sends a POST, returning the response body.
// A non-200 status becomes an error including the status and a truncated body.
func (c *Client) post(ctx context.Context, token *oauth.Credentials, path string, form url.Values) ([]byte, error) {
	ctx = context.WithValue(ctx, oauth.HTTPClient, c.httpClient)
	resp, err := c.oauth.PostContext(ctx, token, c.baseURL+path, form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

// checkAPIError scans a JSON array response for an Instapaper error object.
// Responses that aren't JSON arrays are ignored: a 200 with an unexpected
// body is treated as success since the HTTP status already indicated it.
func checkAPIError(body []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil
	}
	for _, item := range items {
		var e struct {
			Type      string `json:"type"`
			ErrorCode int    `json:"error_code"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(item, &e); err != nil {
			continue
		}
		if e.Type == "error" {
			return &APIError{Code: e.ErrorCode, Message: e.Message}
		}
	}
	return nil
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrBodyLen {
		s = s[:maxErrBodyLen] + "..."
	}
	return s
}
