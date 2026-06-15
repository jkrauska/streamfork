#!/usr/bin/env bash
# Deploy or update streamfork on the LXC. Run inside the container (or via ssh).
#
# Usage:
#   ./scripts/deploy-streamfork.sh              # first deploy
#   ./scripts/deploy-streamfork.sh --pull       # git pull + recreate container

set -euo pipefail

REPO="${STREAMFORK_REPO:-/opt/streamfork}"
REPO_URL="${STREAMFORK_REPO_URL:-https://github.com/jkrauska/streamfork.git}"
COMPOSE_FILE="docker-compose.linux.yml"

PULL=false
for arg in "$@"; do
  case "$arg" in
    --pull) PULL=true ;;
  esac
done

if [[ ! -d "$REPO/.git" ]]; then
  echo "Cloning into ${REPO}..."
  mkdir -p "$(dirname "$REPO")"
  git clone "$REPO_URL" "$REPO"
fi

cd "$REPO"

if $PULL; then
  git pull --ff-only
fi

mkdir -p data/recordings
if [[ ! -f data/streamfork.yml ]]; then
  cp configs/streamfork.example.yml data/streamfork.yml
  echo "Created data/streamfork.yml — edit RTMP URLs and stream keys before enabling outputs."
fi

docker compose -f "$COMPOSE_FILE" up -d --build

echo ""
echo "Streamfork status:"
curl -sf "http://127.0.0.1:8787/healthz" && echo " healthz OK" || echo " healthz not ready yet"
curl -sf "http://127.0.0.1:8787/api/status" | head -c 400 || true
echo ""
echo ""
echo "SRT ingest:  srt://$(hostname -I | awk '{print $1}'):8890  streamid=publish:field"
echo "Control API: http://$(hostname -I | awk '{print $1}'):8787/api/status"
