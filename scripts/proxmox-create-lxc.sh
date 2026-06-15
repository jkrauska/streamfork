#!/usr/bin/env bash
# Run on the Proxmox host as root.
# Creates a privileged Debian 12 LXC tuned for Docker + host-network UDP (SRT).
#
# Prerequisites:
#   - Download "debian-12-standard" template in Proxmox UI (Node → local → CT Templates)
#
# Usage:
#   export VMID=120
#   export STREAMFORK_IP=192.168.1.50/24
#   export GATEWAY=192.168.1.1
#   ./scripts/proxmox-create-lxc.sh

set -euo pipefail

VMID="${VMID:-120}"
HOSTNAME="${HOSTNAME:-streamfork}"
STREAMFORK_IP="${STREAMFORK_IP:-192.168.1.50/24}"
GATEWAY="${GATEWAY:-192.168.1.1}"
BRIDGE="${BRIDGE:-vmbr0}"
STORAGE="${STORAGE:-local-lvm}"
DISK_GB="${DISK_GB:-8}"
MEMORY_MB="${MEMORY_MB:-2048}"
CORES="${CORES:-2}"

TEMPLATE=$(pveam available --section system | awk '/debian-12-standard/ {print $2; exit}')
if [[ -z "${TEMPLATE:-}" ]]; then
  echo "No debian-12-standard template found. Download one in Proxmox UI first."
  exit 1
fi

if ! pveam list local | grep -q "debian-12-standard"; then
  echo "Downloading template ${TEMPLATE}..."
  pveam download local "$TEMPLATE"
fi

LOCAL_TEMPLATE=$(pveam list local | awk '/debian-12-standard/ {print $1; exit}')

if pct status "$VMID" &>/dev/null; then
  echo "CT ${VMID} already exists. Aborting."
  pct list | grep "^${VMID} "
  exit 1
fi

echo "Creating CT ${VMID} (${HOSTNAME}) at ${STREAMFORK_IP}..."

pct create "$VMID" "$LOCAL_TEMPLATE" \
  --hostname "$HOSTNAME" \
  --memory "$MEMORY_MB" \
  --cores "$CORES" \
  --rootfs "${STORAGE}:${DISK_GB}" \
  --net0 "name=eth0,bridge=${BRIDGE},ip=${STREAMFORK_IP},gw=${GATEWAY}" \
  --unprivileged 0 \
  --features nesting=1,keyctl=1 \
  --onboot 1 \
  --start 1

# Docker-friendly settings (privileged CT; safe for a single-purpose appliance CT)
pct set "$VMID" -mp0 /opt,mp=/opt
pct set "$VMID" -tags streamfork,docker

IP="${STREAMFORK_IP%%/*}"
echo ""
echo "Created CT ${VMID}. IP: ${IP}"
echo ""
echo "Next steps:"
echo "  1. pct enter ${VMID}"
echo "  2. curl -fsSL https://raw.githubusercontent.com/jkrauska/streamfork/main/scripts/lxc-install-docker.sh | bash"
echo "     (or copy scripts/lxc-install-docker.sh in and run it)"
echo "  3. Forward UDP 8890 on your router → ${IP}:8890"
echo "  4. Deploy streamfork (see docs/proxmox.md)"
