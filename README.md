<h2 align="center">Netra</h2>

<p align="center">
  <a href="https://github.com/xxddpac/netra/actions/workflows/build.yml"><img src="https://img.shields.io/github/actions/workflow/status/xxddpac/netra/build.yml?branch=main" alt="Build" /></a>
  <img src="https://img.shields.io/github/license/xxddpac/netra" alt="License" />
  <img src="https://img.shields.io/github/go-mod/go-version/xxddpac/netra" alt="Go Version" />
  <img src="https://img.shields.io/github/v/tag/xxddpac/netra" alt="Version" />
  <img src="https://img.shields.io/github/last-commit/xxddpac/netra" alt="Last Commit" />
  <img src="https://img.shields.io/badge/platform-linux-blue" alt="Platform" />
</p>

<p align="center"><b>One binary. Zero dependencies.</b><br />XDP-native capture. Kernel-space aggregation.<br />Ask AI. Extend with MCP.</p>

<p align="center"><a href="README.zh-CN.md">简体中文</a></p>

Netra is a kernel-native traffic observability platform for SPAN and mirror ports. It reads an out-of-band copy of network traffic, never sits on the production forwarding path, and does not require a separate data stack.

## Why Netra

- **Lightweight deployment** — one binary embeds SQLite and DuckDB; no Redis, ClickHouse, Elasticsearch, or other infrastructure components to deploy or maintain.
- **Built for high-volume traffic** — XDP-native capture and kernel-space aggregation keep per-packet work out of userspace.
- **Observe only, never forward** — Netra reads only the traffic copy from a SPAN/mirror port. It does not route, proxy, or modify production traffic.
- **AI grounded in real traffic** — use natural language to query data observed by Netra, then extend the assistant with your own tools and data sources through MCP, such as CMDBs, threat intelligence, or SOC systems.

## Quick Start

1. Download the latest Netra binary from [Releases](https://github.com/xxddpac/netra/releases).
2. Place `GeoLite2-City.mmdb` and `GeoLite2-ASN.mmdb` in the same directory as Netra. Download the GeoLite2 databases by registering at [MaxMind](https://www.maxmind.com/en/geolite2/signup), or use a compatible mirror such as [P3TERX/GeoLite.mmdb](https://github.com/P3TERX/GeoLite.mmdb).
3. Start Netra as root or with the capabilities required to attach an XDP program:

   ```bash
   chmod +x ./netra
   sudo ./netra -iface <mirror-nic>
   # sudo ./netra -iface <mirror-nic> -generic
   ```

4. Open `http://<host>:10211`. On first startup, Netra prints a randomly generated admin password to the log; change it after signing in.

## Screenshots

<img src="docs/login.png" alt="Netra sign-in page" width="100%" />

<p align="center"><sub>Sanitized dashboard demo — topology</sub></p>
<img src="docs/topology.png" alt="Sanitized dashboard demo, topology" width="100%" />

<p align="center"><sub>Sanitized dashboard demo — world-map</sub></p>
<img src="docs/world-map.png" alt="Sanitized dashboard demo, world-map" width="100%" />

## Architecture

Netra receives only the copy replicated by a switch SPAN/mirror port:

<img src="docs/mirror-topology-en.png" alt="Netra traffic mirroring topology" width="100%" />

The processing path consists of kernel-space XDP/eBPF, userspace collection, embedded storage, and the application layer:

<img src="docs/architecture-en.png" alt="Netra architecture" width="100%" />

## Performance

### Traffic capture

Traditional packet capture tools copy every packet to userspace before parsing it, so overhead rises linearly with packet rate. Packet loss occurs when kernel buffers overflow or userspace cannot keep up. Netra attaches XDP at the NIC receive path; capture, parsing, and aggregation stay in kernel space, and userspace reads only already-aggregated results.

The following measurement came from two 10Gbps mirror NICs on a 40-core production host: about 20Gbps / 1.6M pps with zero packet loss at roughly 1.2 CPU cores.

<img src="docs/benchmark-traffic-en.png" alt="Traffic capture performance benchmark" width="100%" />

### Storage and queries

Traffic history is sealed into hourly Parquet files, while per-file Top-K caches are warmed on startup. DuckDB's columnar query engine enables efficient aggregation across large historical datasets without an external time-series database.

<img src="docs/benchmark-duckdb-en.png" alt="DuckDB warm-cache benchmark" width="100%" />

## Features

### Dashboard

- **Live Dashboard** — auto-refreshing overview KPIs, traffic trend, mirror NIC throughput, new-connection rate, top live flows, destination countries, world map, and internal topology. The map can show the world view, topology view, or rotate between them.
- **Insights** — the full dashboard data set in focused panels for protocol, service category, countries, IPs, ports, domains, traffic trend, and connection rate.

### Explore and detect

- **Flow Explorer** — search and page through flows, IPs, ports, domains, service categories, SQL audit records, and weak-credential findings. Flow rows include the inferred initiator/receiver direction from TCP SYN/ACK behavior.
- **IP Profile** — open from an IP in supported tables to inspect traffic totals, peers, protocol/service mix, trend, alert history, and observed service fingerprints.
- **Threat Alerts** — review port/host scans, suspected DDoS, single-IP high traffic, and IOC matches. Alerts can be sent to WeCom, DingTalk, or Feishu and enriched with AI when configured.

### Operations and integrations

- **Settings** — configure refresh and map rotation, threat thresholds, capacity limits, Kafka export, SQL audit, weak-credential detection, IOC watchlists, asset tags, alert channels, and MCP servers.
- **System Monitoring** — inspect host/process health, database and flow-history storage, capture skips, Kafka queue health, and mirror interface/XDP status.
- **User Management** — administrators can manage dashboard accounts and session policies.
- **AI assistant** — ask questions about Netra's observed traffic through an OpenAI-protocol-compatible model.
- **MCP extensions** — let the assistant call your own tools and data sources over HTTP or stdio with optional Basic/Bearer authentication.
- **Kafka** — asynchronously publishes five-tuple traffic records for Grafana or any downstream consumer.
- **Passive enrichment** — GeoIP/ASN, TLS SNI, HTTP Host, IANA port mapping, protocol DPI, and plaintext HTTP service fingerprints.

## Deployment

### Requirements

- `x86_64` (`amd64`) is supported; Linux kernel 4.18+ is recommended.
- The runtime environment needs glibc >= 2.28 (check with `ldd --version`) because DuckDB/CGO use dynamic linking.
- Before using native XDP, check the NIC driver with `ethtool -i <iface>`. After startup, use `ip link show <iface>`: `prog/xdp id ...` indicates native mode; `prog/xdpgeneric id ...` indicates generic mode.

### Flags

| Flag | Required | Default | Description |
|---|---:|---|---|
| `-iface` | Yes | — | Mirror NIC(s), comma-separated. Interfaces share one eBPF map and the view aggregates automatically. |
| `-web-addr` | No | `:10211` | Web dashboard listen address. |
| `-generic` | No | `false` | Force generic/SKB mode. |
| `-interval` | No | `5s` | eBPF map collection and statistics-rollup interval. |
| `-geoip-db` | No | `GeoLite2-City.mmdb` | GeoIP city database path. |
| `-geoip-asn-db` | No | `GeoLite2-ASN.mmdb` | GeoIP ASN database path. |
| `-db` | No | `netra.db` | Persistent database path. |
| `-db-retention` | No | `1m` | Historical retention, formatted as `<N>d` or `<N>m`. |
| `-db-hot-period` | No | `1h` | Interval for sealing hot in-memory history into files. |

### Running under systemd

Create `/etc/systemd/system/netra.service` and replace `ens7f1` with your mirror NIC:

```ini
[Unit]
Description=Netra kernel-native traffic observability
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/netra
ExecStart=/opt/netra/netra -iface ens7f1
User=root
StandardOutput=journal
StandardError=journal
SyslogIdentifier=netra
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Then run:

```bash
systemctl daemon-reload
systemctl start netra
systemctl status netra
journalctl -u netra -f
```

## Build

```bash
cd frontend
npm install
npm run build
cd ..
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o netra .
```
