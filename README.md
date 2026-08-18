# papercast-archiver

A tiny web companion to [papercast](https://github.com/cdzombak/papercast). papercast puts links in each episode's description pointing at this app; opening one shows the article's title and source and offers two actions — **Archive** and **Delete** — which act on the Instapaper article underlying the episode, after a confirmation step.

There is no authentication: the app is designed to run behind Tailscale or a similar secure, single-user reverse proxy. Do not expose it to the public internet.

## Configuration

Configuration is a YAML file, passed via `-config` (default `./config.yaml`). See [`config.example.yaml`](config.example.yaml):

```yaml
server:
  listen: "0.0.0.0:8080"
  base_url: "https://server.my-tailnet.ts.net:8443"

instapaper:
  consumer_key: "your-instapaper-oauth-consumer-key"
  consumer_secret: "your-instapaper-oauth-consumer-secret"
  credentials_file: "/instapaper.json"
```

- `server.listen`: address/port the server listens on.
- `server.base_url`: the complete public base URL at which the app is exposed (i.e. what users' browsers see, through the reverse proxy).
- `instapaper.consumer_key`/`consumer_secret`: an Instapaper OAuth consumer token; [request one here](https://www.instapaper.com/main/request_oauth_consumer_token).
- `instapaper.credentials_file`: path where the Instapaper OAuth token is stored, created by `instapaper-login`.

## Deployment

Docker images for amd64 and arm64 are published to Docker Hub as [`cdzombak/papercast-archiver`](https://hub.docker.com/r/cdzombak/papercast-archiver). See [`docker-compose.example.yml`](docker-compose.example.yml):

```yaml
services:
  papercast-archiver:
    image: cdzombak/papercast-archiver:1
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./config.yaml:/config.yaml:ro
      - ./instapaper.json:/instapaper.json:ro
    restart: unless-stopped
```

### 1. Log in to Instapaper

Before the first run, generate the Instapaper credentials file. This is the only time the credentials file mount needs to be writable (`instapaper.json` must exist first, e.g. `touch instapaper.json`):

```shell
docker compose run --rm -it -v ./instapaper.json:/instapaper.json papercast-archiver instapaper-login -config /config.yaml
```

You'll be prompted for your Instapaper username and password; they're exchanged for a permanent OAuth token saved to `instapaper.json`, and are not stored.

### 2. Start the server

```shell
docker compose up -d
```

### 3. Expose it via Tailscale

```shell
tailscale serve --bg --https=8443 http://127.0.0.1:8080
```

Set `server.base_url` to the resulting URL (e.g. `https://server.my-tailnet.ts.net:8443`), and point papercast's archiver link configuration at the same URL.

## CLI

```
papercast-archiver <command> [flags]
```

- `serve`: run the web server
- `instapaper-login`: interactively authenticate with Instapaper and save credentials
- `healthcheck`: probe the running server's `/_status` endpoint (used by the Docker health check); exit 0 if up, 1 if not
- `version`, `help [command]`

All commands accept `-config <path>` and `-log-level <debug|info|warn|error>` (default `info`).

## License

MIT; see [LICENSE](LICENSE).

## Author

Chris Dzombak ([dzombak.com](https://www.dzombak.com), [github.com/cdzombak](https://github.com/cdzombak)).
