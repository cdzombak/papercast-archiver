package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunUnknownCommand(t *testing.T) {
	if got := run([]string{"bogus"}); got != exitUsage {
		t.Errorf("run(bogus) = %d, want %d", got, exitUsage)
	}
}

func TestRunNoCommand(t *testing.T) {
	if got := run(nil); got != exitUsage {
		t.Errorf("run() = %d, want %d", got, exitUsage)
	}
}

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"version", "-version", "--version"} {
		if got := run([]string{arg}); got != exitSuccess {
			t.Errorf("run(%s) = %d, want %d", arg, got, exitSuccess)
		}
	}
}

func TestRunHelp(t *testing.T) {
	if got := run([]string{"help"}); got != exitSuccess {
		t.Errorf("run(help) = %d, want %d", got, exitSuccess)
	}
	if got := run([]string{"help", "serve"}); got != exitSuccess {
		t.Errorf("run(help serve) = %d, want %d", got, exitSuccess)
	}
	if got := run([]string{"help", "bogus"}); got != exitUsage {
		t.Errorf("run(help bogus) = %d, want %d", got, exitUsage)
	}
}

func TestServeRejectsExtraArgs(t *testing.T) {
	if got := run([]string{"serve", "extra"}); got != exitUsage {
		t.Errorf("run(serve extra) = %d, want %d", got, exitUsage)
	}
}

func TestHealthcheckURL(t *testing.T) {
	tests := []struct {
		listen  string
		want    string
		wantErr bool
	}{
		{"0.0.0.0:8080", "http://127.0.0.1:8080/_status", false},
		{":8080", "http://127.0.0.1:8080/_status", false},
		{"[::]:8080", "http://127.0.0.1:8080/_status", false},
		{"192.168.1.5:9000", "http://192.168.1.5:9000/_status", false},
		{"localhost:1234", "http://localhost:1234/_status", false},
		{"no-port", "", true},
	}
	for _, tt := range tests {
		got, err := healthcheckURL(tt.listen)
		if tt.wantErr {
			if err == nil {
				t.Errorf("healthcheckURL(%q): want error", tt.listen)
			}
			continue
		}
		if err != nil {
			t.Errorf("healthcheckURL(%q): %v", tt.listen, err)
			continue
		}
		if got != tt.want {
			t.Errorf("healthcheckURL(%q) = %q, want %q", tt.listen, got, tt.want)
		}
	}
}

func TestProbeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	if err := probeStatus(srv.URL + "/_status"); err != nil {
		t.Errorf("probeStatus: %v", err)
	}
	if err := probeStatus(srv.URL + "/nope"); err == nil {
		t.Error("probeStatus: want error for 404")
	}
}

func TestProbeStatusNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": false}`))
	}))
	defer srv.Close()

	err := probeStatus(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not ok") {
		t.Errorf("probeStatus = %v, want not-ok error", err)
	}
}

func TestProbeStatusDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	if err := probeStatus(srv.URL); err == nil {
		t.Error("probeStatus: want error for closed server")
	}
}
