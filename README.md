# cekokam-stream-server

A small Go service that polls upstream HLS playlists, downloads + validates `.ts` segments, rewrites the m3u8 with the local segment paths, and serves the result over HTTP. Channel configuration is pulled from the cekokam Laravel dashboard via a bearer-token-protected internal API.

This service replaces a Laravel cron pipeline that did the same job inside `app:download-stream-files`. See `../cekokam-filament-v5/docs/MIGRATION.md` for the cutover plan.

## What it does

For each channel returned by the dashboard's `/api/internal/channels` endpoint where `is_active = true`:

- Polls the upstream m3u8 every `POLL_INTERVAL` (default 12s).
- Downloads each `.ts` segment that isn't already on disk to `${STORAGE_DIR}/streams/<slug>/ts/<seqName>/<md5>.ts`.
- Validates: HTTP 200, non-empty body, body must NOT contain the literal string `Internal Server Error` (some upstreams return 200 with that body). On the marker check, the partial file is removed.
- Rewrites the m3u8 — `.ts` lines pointed at the local hashed paths, `#EXTINF:` lines reformatted as `#EXTINF:<n> tvg-logo="${PUBLIC_URL}/logos/<slug>.png", <name>` — and publishes atomically via `os.Rename`.
- Prunes old sequence folders every `PRUNE_INTERVAL` (default 3m), keeping the latest `PRESERVE_COUNT` (default 50).
- Pulls and caches each channel's logo via `/api/internal/channels/<slug>/logo`, saving as `${STORAGE_DIR}/logos/<slug>.png`.
- Serves the storage directory at `/streams/...`, `/logos/...`, plus `/healthz`.

## Configuration

All config is via env vars.

| Var | Required | Default | Description |
|---|---|---|---|
| `DASHBOARD_URL` | yes | — | Base URL of the Laravel dashboard (e.g. `https://dashboard.cekokam.tld`). Trailing slash trimmed. |
| `DASHBOARD_TOKEN` | yes | — | Bearer token; must match Laravel's `STREAM_SERVER_TOKEN`. |
| `PUBLIC_URL` | yes | — | The Go server's own public base URL (e.g. `https://stream.cekokam.tld`). Embedded into rewritten EXTINF lines as `tvg-logo="…/logos/<slug>.png"`. Must match Laravel's `STREAM_SERVER_PUBLIC_URL`. |
| `STORAGE_DIR` | yes | — | On-disk root for `streams/` and `logos/`. |
| `LISTEN_ADDR` | no | `:8080` | TCP listen address. |
| `POLL_INTERVAL` | no | `12s` | Per-channel upstream m3u8 poll cadence. |
| `PRUNE_INTERVAL` | no | `3m` | Per-channel sequence-folder prune cadence. |
| `PRESERVE_COUNT` | no | `50` | Number of most-recent sequence folders to retain. |
| `SEGMENT_TIMEOUT` | no | `30s` | Per-segment download timeout. |

## Run locally

```bash
DASHBOARD_URL=http://host.docker.internal \
DASHBOARD_TOKEN=test-token \
PUBLIC_URL=http://localhost:8080 \
STORAGE_DIR=/tmp/cekokam-streams \
go run ./cmd/server
```

Then:

```bash
curl http://localhost:8080/healthz
curl -i http://localhost:8080/streams/<slug>/stream.m3u8
```

## Run in Docker (shared-volume deployment)

The Go server is designed to mount the existing Laravel `storage/app/public/` volume so the cutover from the old PHP pipeline is instant — same files, new writer.

```yaml
services:
  stream-server:
    image: ghcr.io/cekokam/stream-server:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      DASHBOARD_URL: https://dashboard.cekokam.tld
      DASHBOARD_TOKEN: ${STREAM_SERVER_TOKEN}
      PUBLIC_URL: https://stream.cekokam.tld
      STORAGE_DIR: /storage
    volumes:
      - /volumes/cekokam-app/storage/app/public:/storage
```

## Endpoints

| Path | Notes |
|---|---|
| `GET /streams/<slug>/stream.m3u8` | rewritten manifest, `Cache-Control: no-cache` |
| `GET /streams/<slug>/ts/<seq>/<hash>.ts` | segment, `Cache-Control: public, max-age=10` |
| `GET /logos/<slug>.png` | logo, `Cache-Control: public, max-age=300` |
| `GET /healthz` | 200 if dashboard sync + at least one channel tick succeeded in the last 2 minutes; else 503 |

## Build

```bash
go build ./...
go vet ./...
go test ./...
```

Multi-stage Docker:

```bash
docker build -t cekokam-stream-server:latest .
```

The runtime image is `gcr.io/distroless/static-debian12:nonroot` — non-root, no shell, no package manager.
