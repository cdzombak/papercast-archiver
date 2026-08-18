# `papercast-archiver` webapp spec
## Project Overview
This application is a simple, tiny companion to papercast (https://github.com/cdzombak/papercast). It allows papercast to include links in episode descriptions to archive or delete the Instapaper article underlying the episode.

The application exposes URLs like `/<id>` where `<id>` is an Instapaper article ID. The user is presented with the article title and source domain, and two buttons: Archive and Delete.

Pressing either button takes the user to a second page with the title, source domain, and a single button that confirms the archive/delete operation. After confirming, the user is brought to a page with a simple success graphic.

Note that this application is intended to run behind Tailscale or a similar, secure, single-user reverse proxy.

## Functional Requirements
### URLs and Query Params
- GET `/<id>?title=foo&domain=bar`: displays the archive/delete choices. Note that article title and domain are embedded in the URL, since the Instapaper API doesn’t allow fetching an individual article by ID. These values are properly escaped for display in the application.
- GET `/<id>/<archive|delete>?title=foo&domain=bar`: displays the archive/delete confirmation (including article title/domain).
- POST `/<id>/<archive|delete>`, with `title=foo&domain=bar` in the POST body: confirms the archive/delete operation. Redirects (303) to `/<id>/<archive|delete>/done?title=foo&domain=bar`.
- GET `/<id>/<archive|delete>/done?title=foo&domain=bar`: confirms the operation took place successfully.

The given title and domain are truncated to 128 runes each, with an ellipsis if needed.

#### GET and POST
Note that the actual archive/delete calls are POSTs, never GETs.

#### Error & edge cases
- Unknown routes, including non-numeric routes, return 404
- Be certain that `/_status` takes precedence over `/<id>/*` in route matching

### Instapaper Authentication
This application is authenticated to Instapaper on the backend, one user per application instance.

Authentication uses Instapaper's xAuth flow (`oauth/access_token` with `x_auth_mode=client_auth`), which exchanges a username/password for a permanent OAuth 1.0a token pair. Credentials are supplied once via a `instapaper-login` CLI mode (interactive prompt for username/password). OAuth client key and secret are provided in the `instapaper` section of the config. The resulting token and token secret are persisted at a path specified by the user for Instapaper credentials via the config.

The end-user facing process is documented in the README.

The implementing agent should research and choose an appropriate OAuth library rather than writing the OAuth process from scratch.

The Instapaper credential file is JSON format and created with 0600 permissions.

#### Suggested Workflow
The end user is intended to run the application initially with `instapaper-login -config <config path>` , to go through the Instapaper authentication process and generate the Instapaper auth file.

### Unauthenticated Access
This application is intended to run behind Tailscale or a similar, secure, single-user reverse proxy. The README makes this clear. Therefore, the application performs no authentication.

### Error Handling
If any error occurs, particularly interacting with the Instapaper API, log it, and display a failure page with error details to the user.

This is a trivial, single-user application, so no extra effort (e.g. retries) or attempts at obfuscating error details are required.

### Color & Design
The application uses a minimal, bright, slightly off-white (in light mode) color scheme that closely matches Instapaper’s.

Destructive actions (e.g. confirming intention to archive or delete) are colored in red.

Messages, buttons, and feedback are always displayed in the center of the screen, optimizing for use on phones. Feedback is never via a JavaScript alert.

#### Dark Mode
The app’s UI supports both dark & light mode, following the system-level preference of the user’s device.

#### Responsive Design
The app will likely be used heavily on mobile. The app therefore has first-class support for both desktop and phone-sized devices, including touch-friendly button sizes.

### Base URL
The application’s base URL (e.g. `https://server.my-tailnet.ts.net:1234` ) is provided via the config.

This is the complete base URL at which the papercast-archiver application is exposed. The application uses this when necessary to build links/URLs. (Take care not to introduce double slashes in the URL, should the user include a trailing slash.)

### Listen address/port
The config file specifies the address/port on which to listen.

### Status Endpoint
The endpoint `/_status` returns a simple JSON body, `{“ok”: true}`, that a status checker can check.

If the CLI mode `healthcheck` is given, the program probes the running server's `/_status` using the listen address from the config, then exits 0 if it is up, 1 if not. Used by the Docker health check.

### CLI
The CLI has three subcommands: `instapaper-login`, `healthcheck` and `serve`. They all require a valid config. In addition, `help` and `version` are accepted; see papercast ( https://github.com/cdzombak/papercast ) for an example to follow.

A `-log-level` flag allows changing log level; it defaults to info.

### Configuration
Configuration is via a YAML file, the path to which is given by the `-config` flag. It defaults to `./config.yaml`. Configuration is broken into logical sections e.g. `instapaper`, `server`.

Configuration is required for all modes except `help` and `version`, and is validated on startup. Failures print a helpful error message and cause the program to exit with nonzero exit code.

### Logging
All requests are logged at debug level. POSTs that successfully change article state are logged at info level. Any failed Instapaper API interactions are logged at error level.

## Implementation Details
- Language: Go
- Network/HTTP library: Go standard library
- Templating: Go standard library
- Logging: Go stdlib `slog`

### Data Model
This application retrieves data from Instapaper via its API as needed; no data persistence, and therefore no long-term data model, is needed.

### Build Process & Artifacts
A Makefile is provided that can run:
- lint (golangci-lint, plus any other linters that might be helpful for this application)
- actionlint (for GitHub Actions YAML)
- test (runs the test suite)
- build-docker (builds a Docker image for the current machine)
- clean (cleans `./out`)

GitHub Actions CI workflows are provided that:
- On push or PRs, runs lint, actionlint, and the test suite
- On tags like `vX.Y.Z`, requires lint and the test suite to pass, then builds Docker images for arm64 and amd64 and pushes them to Docker Hub
- Creates a GitHub release
- Updates rolling `vX` and `vX.Y` release tags

The version number is embedded in the binary and the Docker image.

All template files are embedded in the Go binary via `go:embed`.

The implementing agent should refer to https://github.com/cdzombak/ebird-rss for an example Makefile and GitHub Actions setup that I prefer.

### Deployment
- Deployment is left to users, not accomplished by an AI agent
- The app is intended to run behind a Tailscale or similar reverse proxy; the app itself does not handle e.g. TLS termination
- The repo provides an example Docker Compose file suitable for deployment
- The repo provides an example `tailscale serve` command, which the user can easily modify as needed

### Dockerfile
- Use UID/GID 1000 inside the Dockerfile.
- Users are expected to provide 2 mounts: the Instapaper credentials file and the config file (both readonly, except for the initial `instapaper-login` run which needs to write to the Instapaper creds file)
- The `/_status` endpoint is used for Docker health checks

The Dockerfile is based on `scratch` to minimize size. One item requires special handling:
- `COPY --from=builder /etc/ssl/certs/ca-certificates.crt`, allowing the app to connect to HTTPS hosts

### README
The README covers:
- A (short) overview of the app
- Instructions on configuring & deploying the app
- A link to github.com/cdzombak/papercast, for context/orientation

### Tests
Tests never touch the network or a real Instapaper account. The relevant Instapaper APIs are stubbed with `httptest.Server` returning canned fixtures. Additionally, the Instapaper API sits behind an interface and a mock is injected for testing anything that depends on it.
