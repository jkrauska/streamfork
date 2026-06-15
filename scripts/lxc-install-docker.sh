#!/usr/bin/env bash
# Run inside the streamfork LXC as root (after pct enter <vmid>).
# Installs Docker CE + compose plugin on Debian 12.

set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root inside the LXC."
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y ca-certificates curl git

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" \
  > /etc/apt/sources.list.d/docker.list

apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable --now docker

# Verify host networking works (required for SRT)
docker run --rm --network host alpine:3.20 true

echo ""
echo "Docker installed: $(docker --version)"
echo "Compose: $(docker compose version)"
echo ""
echo "Deploy streamfork:"
echo "  git clone https://github.com/jkrauska/streamfork.git /opt/streamfork"
echo "  cd /opt/streamfork && cp configs/streamfork.example.yml data/streamfork.yml"
echo "  docker compose -f docker-compose.linux.yml up -d --build"
