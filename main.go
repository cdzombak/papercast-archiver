// papercast-archiver is a tiny web companion to papercast that archives or
// deletes the Instapaper article underlying a podcast episode.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/cdzombak/papercast-archiver/internal/config"
	"github.com/cdzombak/papercast-archiver/internal/instapaper"
	"github.com/cdzombak/papercast-archiver/internal/server"
)

var version = "<dev>"

const (
	exitSuccess = 0
	exitFailure = 1
	// exitUsage is returned for invalid command lines (unknown command, bad flags).
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "papercast-archiver: a command is required")
		printUsage(os.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "instapaper-login":
		return cmdLogin(args[1:])
	case "healthcheck":
		return cmdHealthcheck(args[1:])
	case "version", "-version", "--version":
		fmt.Println("papercast-archiver " + version)
		return exitSuccess
	case "help", "-h", "-help", "--help":
		return cmdHelp(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "papercast-archiver: unknown command %q\n", args[0])
		fmt.Fprintln(os.Stderr, `Run "papercast-archiver help" for usage.`)
		return exitUsage
	}
}

func cmdHelp(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return exitSuccess
	}
	switch args[0] {
	case "serve":
		return cmdServe([]string{"-h"})
	case "instapaper-login":
		return cmdLogin([]string{"-h"})
	case "healthcheck":
		return cmdHealthcheck([]string{"-h"})
	case "version":
		fmt.Println("Usage: papercast-archiver version\n\nPrint the papercast-archiver version and exit.")
		return exitSuccess
	case "help":
		printUsage(os.Stdout)
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "papercast-archiver help: unknown command %q\n", args[0])
		printUsage(os.Stderr)
		return exitUsage
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `papercast-archiver serves a tiny web UI to archive or delete the Instapaper
article underlying a papercast episode.

Usage:

  papercast-archiver <command> [flags]

Commands:

  serve             run the web server
  instapaper-login  interactively authenticate with Instapaper and save credentials
  healthcheck       probe the running server's /_status endpoint; exit 0 if up, 1 if not
  version           print the version and exit
  help [command]    print usage for papercast-archiver or a single command

Run "papercast-archiver help <command>" for details on a command's flags.
`)
}

// addCommonFlags registers the -config and -log-level flags shared by every command.
func addCommonFlags(fs *flag.FlagSet) (configPath, logLevel *string) {
	configPath = fs.String("config", "./config.yaml", "path to the config YAML file")
	logLevel = fs.String("log-level", "info", "log level: debug, info, warn, or error")
	return configPath, logLevel
}

// usageFor returns a usage printer for fs: synopsis, then the flag defaults.
func usageFor(fs *flag.FlagSet, synopsis string) func(io.Writer) {
	return func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "%s\n\nFlags:\n", synopsis)
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(os.Stderr)
	}
}

// parseFlags parses args into fs. It returns (exit, true) when the command
// should stop immediately: exit 0 after -h/--help (usage printed to stdout),
// exit 2 on a parse error or unexpected positional argument (error and usage
// printed to stderr). Returns (0, false) when parsing succeeded.
func parseFlags(fs *flag.FlagSet, args []string, usage func(io.Writer)) (int, bool) {
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {}
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		usage(os.Stdout)
		return exitSuccess, true
	case err != nil:
		usage(os.Stderr)
		return exitUsage, true
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "papercast-archiver %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		usage(os.Stderr)
		return exitUsage, true
	}
	return 0, false
}

// initCommand builds the logger and loads the config. It reports its own
// errors; on failure the caller should return exitFailure.
func initCommand(configPath, logLevel string) (*config.Config, *slog.Logger, bool) {
	logger, err := newLogger(logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, false
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("configuration error", "error", err)
		return nil, nil, false
	}
	return cfg, logger, true
}

func newLogger(level string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid -log-level %q (use debug, info, warn, or error)", level)
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})), nil
}

func cmdServe(args []string) int {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath, logLevel := addCommonFlags(fset)
	usage := usageFor(fset, `Usage: papercast-archiver serve [flags]

Run the web server on the address configured at server.listen.`)
	if exit, done := parseFlags(fset, args, usage); done {
		return exit
	}

	cfg, logger, ok := initCommand(*configPath, *logLevel)
	if !ok {
		return exitFailure
	}

	creds, err := instapaper.LoadCredentials(cfg.Instapaper.CredentialsFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logger.Error(`instapaper credentials not found; run "papercast-archiver instapaper-login" first`,
				"path", cfg.Instapaper.CredentialsFile)
		} else {
			logger.Error("load instapaper credentials", "error", err)
		}
		return exitFailure
	}
	api := instapaper.NewClient(cfg.Instapaper.ConsumerKey, cfg.Instapaper.ConsumerSecret, creds)
	srv := server.New(api, cfg.Server.BaseURL, logger)

	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Server.Listen, "base_url", cfg.Server.BaseURL, "version", version)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", "error", err)
		return exitFailure
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
			return exitFailure
		}
	}
	return exitSuccess
}

func cmdLogin(args []string) int {
	fs := flag.NewFlagSet("instapaper-login", flag.ContinueOnError)
	configPath, logLevel := addCommonFlags(fs)
	usage := usageFor(fs, `Usage: papercast-archiver instapaper-login [flags]

Interactively authenticate with Instapaper and save the resulting OAuth token
to the path configured at instapaper.credentials_file.`)
	if exit, done := parseFlags(fs, args, usage); done {
		return exit
	}

	cfg, logger, ok := initCommand(*configPath, *logLevel)
	if !ok {
		return exitFailure
	}

	// Deliberately not using signal.NotifyContext here: it intercepts
	// SIGINT and turns off the default kill-the-process behavior, but
	// nothing in the blocking stdin prompts below would ever observe
	// context cancellation, so Ctrl-C would appear to do nothing.
	if err := runLogin(context.Background(), cfg); err != nil {
		logger.Error("instapaper login failed", "error", err)
		return exitFailure
	}
	return exitSuccess
}

// runLogin performs the interactive xAuth flow and saves the resulting token.
func runLogin(ctx context.Context, cfg *config.Config) error {
	fmt.Print("Instapaper username (email): ")
	reader := bufio.NewReader(os.Stdin)
	username, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read username: %w", err)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username must not be empty")
	}

	fmt.Print("Instapaper password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}

	client := instapaper.NewClient(cfg.Instapaper.ConsumerKey, cfg.Instapaper.ConsumerSecret, nil)
	creds, err := client.RequestAccessToken(ctx, username, string(passwordBytes))
	if err != nil {
		return err
	}
	if err := instapaper.SaveCredentials(cfg.Instapaper.CredentialsFile, creds); err != nil {
		return err
	}
	fmt.Printf("Instapaper credentials saved to %s\n", cfg.Instapaper.CredentialsFile)
	return nil
}

func cmdHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	configPath, logLevel := addCommonFlags(fs)
	usage := usageFor(fs, `Usage: papercast-archiver healthcheck [flags]

Probe the running server's /_status endpoint using the listen address from the
config. Exits 0 if the server is up, 1 if not.`)
	if exit, done := parseFlags(fs, args, usage); done {
		return exit
	}

	cfg, logger, ok := initCommand(*configPath, *logLevel)
	if !ok {
		return exitFailure
	}

	url, err := healthcheckURL(cfg.Server.Listen)
	if err != nil {
		logger.Error("healthcheck failed", "error", err)
		return exitFailure
	}
	if err := probeStatus(url); err != nil {
		logger.Error("healthcheck failed", "url", url, "error", err)
		return exitFailure
	}
	return exitSuccess
}

// healthcheckURL builds the /_status URL for a listen address, substituting
// 127.0.0.1 for wildcard hosts.
func healthcheckURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", listen, err)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/_status", nil
}

// probeStatus fetches url and verifies the {"ok": true} response.
func probeStatus(url string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var status struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}
	if !status.OK {
		return errors.New("status response is not ok")
	}
	return nil
}
