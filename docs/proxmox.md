# Proxmox relay setup

Run streamfork on a **dedicated LXC** on Proxmox — not Docker on the hypervisor host itself. Proxmox shares the kernel with LXCs; keeping Docker inside an LXC (or VM) avoids fighting `apt` on the node and makes backups/snapshots easy.

```
Starlink Mini ──SRT/UDP──► router ──► streamfork LXC (:8890)
                                      └── docker (network_mode: host)
                                            └── mediamtx + streamfork
```

Proxmox node: e.g. **192.168.1.10**  
Give the LXC its **own IP** (example **192.168.1.50**). Port-forward your router to the **LXC IP**, not the hypervisor.

## 1. Create the LXC (on Proxmox host)

In Proxmox UI: **Node → Create CT**

| Setting | Value |
|---------|--------|
| Template | debian-12-standard |
| Hostname | streamfork |
| Cores / RAM | 2 / 2048 MB |
| Disk | 8 GB |
| IPv4 | e.g. `192.168.1.50/24`, gw `192.168.1.1` |
| Privileged | **Yes** (simplest for Docker) |
| Features | nesting, keyctl |

Or from the Proxmox shell (after copying this repo or scripts):

```bash
export VMID=120
export STREAMFORK_IP=192.168.1.50/24
export GATEWAY=192.168.1.1
./scripts/proxmox-create-lxc.sh
```

## 2. Install Docker (inside the LXC)

```bash
pct enter 120   # or ssh root@192.168.1.50
bash /path/to/lxc-install-docker.sh
```

Or one-liner:

```bash
curl -fsSL https://raw.githubusercontent.com/jkrauska/streamfork/main/scripts/lxc-install-docker.sh | bash
```

## 3. Deploy streamfork

```bash
git clone https://github.com/jkrauska/streamfork.git /opt/streamfork
cd /opt/streamfork
cp configs/streamfork.example.yml data/streamfork.yml
# edit data/streamfork.yml — RTMP keys, enable outputs

docker compose -f docker-compose.linux.yml up -d --build
```

Or:

```bash
./scripts/deploy-streamfork.sh
```

Uses **`network_mode: host`** so MediaMTX binds `:8890` directly on the LXC — no docker-proxy UDP bugs.

## 4. Router port forward

| Protocol | External | Internal |
|----------|----------|----------|
| UDP | 8890 | **192.168.1.50**:8890 |

Optional: TCP 8787 for control API from LAN only (do not expose publicly without auth — M2 adds login).

## 5. Verify

On the LXC:

```bash
curl -s http://127.0.0.1:8787/api/status | jq .
ss -ulnp | grep 8890    # mediamtx listening
```

From the field (Mini):

```
srt://<your-public-ip>:8890
streamid: publish:field
latency: 3000 ms
```

While connecting, on the LXC:

```bash
tcpdump -i eth0 -n 'udp port 8890' -c 10
```

You should see **bidirectional** traffic to the Starlink IP (not mangled docker bridge IPs).

## Why LXC + Docker, not Docker on Proxmox?

| Approach | Verdict |
|----------|---------|
| Docker on Proxmox **host** | Avoid — conflicts with Proxmox packaging |
| **LXC + Docker** | Lightweight, snapshots, good fit |
| Full VM + Docker | Fine but heavier (~512MB+ extra overhead) |
| LXC, binaries only (no Docker) | Lightest; more manual upgrades |

## Updates

```bash
cd /opt/streamfork
./scripts/deploy-streamfork.sh --pull
```

## Resource budget

HEVC copy path: CPU is trivial. **2 cores / 2 GB RAM / 8 GB disk** is plenty for one ingest + a few RTMP outputs + local recording (recording disk may need a separate mount if you keep many hours).
