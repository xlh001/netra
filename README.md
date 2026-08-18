<h2 align="center">Netra</h2>

<p align="center">
  <img src="https://img.shields.io/github/license/xxddpac/netra" alt="License" />
  <img src="https://img.shields.io/github/go-mod/go-version/xxddpac/netra" alt="Go Version" />
  <img src="https://img.shields.io/github/v/tag/xxddpac/netra" alt="Version" />
  <img src="https://img.shields.io/github/last-commit/xxddpac/netra" alt="Last Commit" />
  <img src="https://img.shields.io/badge/platform-linux-blue" alt="Platform" />
  <img src="https://img.shields.io/github/stars/xxddpac/netra?style=social" alt="Stars" />
</p>

<p align="center">One binary. Observe your network with eBPF. Ask AI. Extend with MCP</p>

## 介绍

Netra 是一个旁路部署在镜像网卡的流量可视化平台，不侵入业务链路。单文件二进制即可运行。

## 架构

<img src="docs/architecture.svg" alt="Netra 架构图" width="100%" />

## 流量采集性能

传统抓包（libpcap、tcpdump、AF_PACKET 等）每个包都要拷贝到用户态再逐包解析，开销随**包速率**线性增长，大流量场景下一旦用户态处理跟不上或内核缓冲区被打满，就会丢包。Netra 把 XDP 程序挂在网卡驱动的收包路径上，原生模式下甚至运行在内核分配 `sk_buff` 之前——采集、解包、统计全程留在内核态，用户态只需要读取已经聚合好的结果。这意味着 CPU 开销不再随包速率线性增长，高包速率场景下也不会因为用户态处理跟不上而丢包。
基于 XDP 内核态收包路径本身的量级上限，性能方面处理10Gbps、1Mpps 理论上会留有充分余量，后续将补上Netra实际压测数据。

## 存储查询性能

流量历史（五元组/IP/端口/域名）按小时封存为 Parquet 文件，查询时交给 DuckDB 的列式引擎做聚合——相比引入如 ClickHouse 这类外部时序存储，DuckDB 内嵌在同一个二进制里，不需要额外部署运维。

但五元组这个维度基数天然接近原始行数（源端口很少重复），为此做了几次压测和优化：查询时按封存文件缓存 Top-K 结果，覆盖任意自定义时间范围；服务启动时后台并发预热已有历史文件的缓存；DuckDB 连接按可用内存动态设置了 `memory_limit`，避免极端数据量下的聚合查询把整个进程拖垮。

经优化后 1亿+ 条数据查询稳定在 2-3 秒。

## 支持的功能

- **实时大屏**：总流量、协议占比、流量趋势、Top IP/端口/域名排名、目标国家分布、世界地图、内网拓扑图。
- **流量详情**：按流量、IP、端口、域名、服务分类等多维度数据。
- **威胁感知**：扫描/DDoS/单 IP 大流量三类启发式检测，命中可推送到企业微信/钉钉/飞书，启用 AI 后附带自然语言解读。
- **域名识别**：被动解析 TLS SNI。
- **GeoIP 富化**：接入 MaxMind GeoLite2，标注公网 IP 的国家与归属组织。
- **持久化**：SQLite + DuckDB 混合存储——低频的配置/用户/告警等数据走 SQLite，高频的流量历史（IP/端口/域名/五元组）走 DuckDB，按时间片滚动存为 Parquet 文件。
- **AI 助手**：可接入任意 OpenAI 协议兼容模型，基于真实历史数据回答问题。
- **MCP 扩展**：可接入外部 MCP Server，AI 助手对话时可按需调用其提供的工具，支持 HTTP/stdio 两种传输方式及 Basic/Bearer 认证。
- **Kafka**：启用后异步推送流量至队列，自由消费使用。

## 演示


https://github.com/user-attachments/assets/7be16b4e-27a3-46bc-b485-7bfea1c40b95



## 部署

### 环境要求

- Linux 内核，建议 4.18 及以上（`uname -r`）；版本越新，原生 XDP 的驱动兼容性通常越好。
- 是否可以用原生 XDP，取决于内核版本、网卡驱动、硬件收发队列余量，三者缺一不可，判断步骤：
  1. `ethtool -i <iface>` 查看网卡驱动（如 ixgbe、i40e、mlx5、virtio_net 等），主流驱动大多支持原生 XDP，但具体表现因驱动而异；
  2. 启动 Netra 时先不加 `-generic`，跑起来后用 `ip link show <iface>` 看结果——输出里带 `prog/xdp id ...` 说明原生模式挂载成功；带 `prog/xdpgeneric id ...`，或启动日志里有挂载失败的报错，需要回退到了通用模式；
  3. 在测试中遇到过网卡驱动本身支持原生 XDP、但硬件收发队列已经被占满导致挂载失败的情况（如 ixgbe），这种即便内核和驱动都没问题，也只能用通用模式。
- **运行环境需要 glibc >= 2.28**（`ldd --version`）—— DuckDB/CGO 引入动态链接依赖。

### 启动参数

| 参数              | 必填  | 默认值        | 说明                                |
|-----------------|-----|------------|-----------------------------------|
| `-iface`        | 是   | -          | 要挂载 XDP 程序的网卡名                    |
| `-web-addr`     | 否   | `:10211`   | WEB监听地址                           |
| `-generic`      | 否   | false      | 开启后强制使用通用/SKB 模式                  |
| `-interval`     | 否   | 5s         | 采集周期，多久读取一次 eBPF map 并滚动一次统计桶     |
| `-geoip-db`     | 否   | `GeoLite2-City.mmdb`（当前目录） | 用于dashboard世界地图。                  |
| `-geoip-asn-db` | 否   | `GeoLite2-ASN.mmdb`（当前目录）  | 用于标记公网IP的组织信息。                    |
| `-db`           | 否   | `netra.db`（当前目录） | DB文件路径，用于持久化                      |
| `-db-retention` | 否   | `1m`（1 个月） | 历史数据保留时长，格式为 `<N>d`（天）或 `<N>m`（月） |
| `-db-hot-period` | 否   | 1h         | 流量历史从内存热缓冲封存为文件的周期                |

GeoIP 的两个 `.mmdb` 文件 需自行获取，[MaxMind 官网](https://www.maxmind.com/en/geolite2/signup)注册账号下载或第三方镜像仓库 [P3TERX/GeoLite.mmdb](https://github.com/P3TERX/GeoLite.mmdb) 免注册下载，放到 netra 二进制同目录下即可自动识别，不需要显式传参；放在别的位置才需要用 `-geoip-db`/`-geoip-asn-db` 指定绝对路径。

### systemd 常驻运行

1. 获取 `netra` 二进制——从 [Releases](https://github.com/xxddpac/netra/releases) 页面下载。创建工作目录，把 `netra` 二进制和 `GeoIP` 的两个 `.mmdb` 文件都放进去

   ```ini
   mkdir -p /opt/netra
   cp netra GeoLite2-City.mmdb GeoLite2-ASN.mmdb /opt/netra/
   chmod +x /opt/netra/netra
   ```

2. 把下面内容保存为 `/etc/systemd/system/netra.service` ( `-iface` 后面的网卡换成实际网卡名 )

   ```ini
   [Unit]
   Description=One binary. Observe your network with eBPF. Ask AI. Extend with MCP.
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

3. 加载并启动：

   ```ini
   systemctl daemon-reload
   systemctl start netra
   systemctl status netra
   journalctl -u netra -f
   ```

**首次启动** 会在日志里打印随机生成的管理员admin账号密码，登录系统后自行修改。

## 编译

Netra 只能运行在 Linux 上（XDP 是 Linux 内核特性），且流量历史存储引擎（DuckDB）依赖 CGO，需要在目标平台原生编译（Linux + C工具链）

```ini
cd frontend
npm install
npm run build
cd ..
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o netra .
```

<sub>如果这个项目对你有帮助，可以[请作者喝杯咖啡](docs/sponsor.jpg) ☕。</sub>
