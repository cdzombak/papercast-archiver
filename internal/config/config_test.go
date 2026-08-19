package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
server:
  listen: "127.0.0.1:8080"
  base_url: "https://server.my-tailnet.ts.net:1234"
instapaper:
  consumer_key: "key"
  consumer_secret: "secret"
  credentials_file: "/data/instapaper.json"
`

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q", cfg.Server.Listen)
	}
	if cfg.Server.BaseURL != "https://server.my-tailnet.ts.net:1234" {
		t.Errorf("BaseURL = %q", cfg.Server.BaseURL)
	}
	if cfg.Instapaper.ConsumerKey != "key" || cfg.Instapaper.ConsumerSecret != "secret" {
		t.Errorf("Instapaper = %+v", cfg.Instapaper)
	}
	if cfg.Instapaper.CredentialsFile != "/data/instapaper.json" {
		t.Errorf("CredentialsFile = %q", cfg.Instapaper.CredentialsFile)
	}
}

func TestLoadTrimsTrailingSlash(t *testing.T) {
	cfg, err := Load(writeConfig(t, strings.Replace(validConfig,
		"https://server.my-tailnet.ts.net:1234", "https://example.com/", 1)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q, want trailing slash removed", cfg.Server.BaseURL)
	}
}

func TestPathPrefix(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://example.com", ""},
		{"https://example.com:8443", ""},
		{"https://example.com/secret", "/secret"},
		{"https://example.com/a/b", "/a/b"},
	}
	for _, tt := range tests {
		cfg, err := Load(writeConfig(t, strings.Replace(validConfig,
			"https://server.my-tailnet.ts.net:1234", tt.baseURL, 1)))
		if err != nil {
			t.Fatalf("Load(base_url=%q): %v", tt.baseURL, err)
		}
		if got := cfg.Server.PathPrefix(); got != tt.want {
			t.Errorf("PathPrefix() for %q = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestPathPrefixTrimsTrailingSlash(t *testing.T) {
	cfg, err := Load(writeConfig(t, strings.Replace(validConfig,
		"https://server.my-tailnet.ts.net:1234", "https://example.com/secret/", 1)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Server.PathPrefix(); got != "/secret" {
		t.Errorf("PathPrefix() = %q, want %q", got, "/secret")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	if _, err := Load(writeConfig(t, validConfig+"\nbogus: true\n")); err == nil {
		t.Fatal("want error for unknown field")
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{"missing listen", func(s string) string {
			return strings.Replace(s, `listen: "127.0.0.1:8080"`, `listen: ""`, 1)
		}, "server.listen is required"},
		{"missing base_url", func(s string) string {
			return strings.Replace(s, `base_url: "https://server.my-tailnet.ts.net:1234"`, `base_url: ""`, 1)
		}, "server.base_url is required"},
		{"relative base_url", func(s string) string {
			return strings.Replace(s, "https://server.my-tailnet.ts.net:1234", "server.example.com", 1)
		}, "server.base_url must be an absolute http(s) URL"},
		{"base_url with query", func(s string) string {
			return strings.Replace(s, "https://server.my-tailnet.ts.net:1234", "https://example.com/x?y=1", 1)
		}, "server.base_url must not contain a query or fragment"},
		{"base_url with fragment", func(s string) string {
			return strings.Replace(s, "https://server.my-tailnet.ts.net:1234", "https://example.com/x#y", 1)
		}, "server.base_url must not contain a query or fragment"},
		{"missing consumer_key", func(s string) string {
			return strings.Replace(s, `consumer_key: "key"`, `consumer_key: ""`, 1)
		}, "instapaper.consumer_key is required"},
		{"missing consumer_secret", func(s string) string {
			return strings.Replace(s, `consumer_secret: "secret"`, `consumer_secret: ""`, 1)
		}, "instapaper.consumer_secret is required"},
		{"missing credentials_file", func(s string) string {
			return strings.Replace(s, `credentials_file: "/data/instapaper.json"`, `credentials_file: ""`, 1)
		}, "instapaper.credentials_file is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.mutate(validConfig)))
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}
