# streamfork

Self-hosted SRT ingest → multi-RTMP fan-out relay with local recording and a control API.

See [PLAN.md](PLAN.md) for architecture, design decisions, and milestones.

## Ports

All services use a single **88xx block** (easy to remember alongside SRT **8890**):

| Port | Service |
|------|---------|
| **8787** | streamfork control API |
| **8797** | MediaMTX control API (internal) |
| **8798** | MediaMTX metrics (internal) |
| **8854** | RTSP preview (`rtsp://host:8854/field`) |
| **8890** | SRT ingest (UDP) |
| **8935** | RTMP (local/debug) |

RTSP and RTMP use **88** + the classic port suffix (**554** / **935** from 1935).

## Quick start (Docker)

**Mac (dev, API only — SRT from WAN broken in Docker Desktop):**

```bash
cp configs/streamfork.example.yml data/streamfork.yml
make run
```

For SRT ingest from another machine on your LAN, set your host IP when starting Docker:

```bash
HOST_IP=192.168.1.100 make run
```

**Proxmox / Linux relay (production — SRT works):**

See **[docs/proxmox.md](docs/proxmox.md)** for LXC + Docker setup on your Proxmox box.

```bash
docker compose -f docker-compose.linux.yml up -d --build
```

Magewell Mini SRT caller settings (starting point):

| Setting | Value |
|---------|-------|
| URL | `srt://<relay-host>:8890` |
| streamid | `publish:field` (must match `input.path` in config) |
| Latency | 3000 ms |

## Web UI

Open the control API root in a browser (default [http://localhost:8787/](http://localhost:8787/)) to enable or disable output streams, edit destinations, and view a live input preview. Status refreshes every few seconds; the preview updates about every 5 seconds while the input is online.

## Control API (M1)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Liveness |
| GET | `/api/status` | Input SRT stats + output worker states |
| GET | `/api/input/snapshot.jpg` | Latest JPEG preview frame (when input is online) |
| GET | `/api/outputs` | List outputs (stream keys redacted) |
| POST | `/api/outputs` | Add output |
| PUT | `/api/outputs/{id}` | Update output (partial patch) |
| DELETE | `/api/outputs/{id}` | Remove output |
| POST | `/api/outputs/{id}/start` | Start one output worker |
| POST | `/api/outputs/{id}/stop` | Stop one output worker |
| POST | `/api/outputs/{id}/restart` | Restart one output worker |

Example:

```bash
curl -s localhost:8787/api/status | jq .
curl -X POST localhost:8787/api/outputs/youtube/start
```

## Development

```bash
go test ./...
```

### Native on macOS (no Docker)

Best for local SRT ingest from Starlink — avoids Docker Desktop UDP bugs.

```bash
make deps-macos    # once: Homebrew ffmpeg + MediaMTX binary in bin/
make run-local     # builds streamfork, runs mediamtx + app on the Mac
```

Uses `data/streamfork.local.yml` (copied from `configs/streamfork.local.yml` on first run).

- Control API: http://localhost:8787/api/status  
- RTSP preview: `rtsp://127.0.0.1:8854/field`  
- SRT ingest: `srt://<your-mac-lan-ip>:8890` · streamid `publish:field`

Edit outputs in `data/streamfork.local.yml`, then restart.

**Do not commit** `data/` — it holds local configs (stream keys) and recordings. Templates live under `configs/`.

### Docker on Mac

API/dev only — SRT from WAN is broken under Docker Desktop (see docs/proxmox.md for relay).

```bash
make run
```

## Status

- **M0** — manual spike (field SRT → ffmpeg copy to destinations)
- **M1** — control app, supervised ffmpeg workers, REST API, recording via MediaMTX, container build *(in progress)*
- **M2** — password-protected Web UI
- **M3** — slate fallback, Slack notifications, metrics passthrough, hot-reload polish
