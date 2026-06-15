# PLAN.md — SRT Ingest → Multi-RTMP Fan-out Relay

> Working name: **streamfork** (rename freely). A self-hosted relay that takes a
> single jitter-protected SRT feed from the field, records it locally, and fans it
> out to multiple RTMP/RTMPS destinations (GameChanger, YouTube, …) as independent,
> individually-restartable streams — with a web UI for live observability and control.

See the full plan in the repository history / project docs. Milestones:

- **M0** — Spike: manual SRT → ffmpeg copy path
- **M1** — MVP control app (current): supervised workers, REST API, recording, container
- **M2** — Web UI with auth
- **M3** — Resilience & polish (slate, Slack, metrics, hot-reload)

Implementation layout matches §11 of the original plan under `cmd/`, `internal/`, and `configs/`.
