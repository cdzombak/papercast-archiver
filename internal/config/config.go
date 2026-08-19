// Package config loads and validates papercast-archiver's YAML configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Instapaper InstapaperConfig `yaml:"instapaper"`
}

type ServerConfig struct {
	Listen  string `yaml:"listen"`
	BaseURL string `yaml:"base_url"`
}

type InstapaperConfig struct {
	ConsumerKey     string `yaml:"consumer_key"`
	ConsumerSecret  string `yaml:"consumer_secret"`
	CredentialsFile string `yaml:"credentials_file"`
}

// Load reads and validates the config file at path. The base URL is
// normalized to have no trailing slash.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Server.BaseURL = strings.TrimRight(cfg.Server.BaseURL, "/")
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

// PathPrefix returns the path component of BaseURL with no trailing slash,
// or "" when the app is served at the root. All links and redirects the
// server generates are placed under this prefix.
func (s ServerConfig) PathPrefix() string {
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.EscapedPath(), "/")
}

func (c *Config) validate() error {
	var errs []string
	req := func(v, name string) {
		if strings.TrimSpace(v) == "" {
			errs = append(errs, name+" is required")
		}
	}

	req(c.Server.Listen, "server.listen")
	req(c.Server.BaseURL, "server.base_url")
	req(c.Instapaper.ConsumerKey, "instapaper.consumer_key")
	req(c.Instapaper.ConsumerSecret, "instapaper.consumer_secret")
	req(c.Instapaper.CredentialsFile, "instapaper.credentials_file")

	if c.Server.BaseURL != "" {
		u, err := url.Parse(c.Server.BaseURL)
		switch {
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
			errs = append(errs, "server.base_url must be an absolute http(s) URL")
		case u.RawQuery != "" || u.Fragment != "":
			errs = append(errs, "server.base_url must not contain a query or fragment")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
