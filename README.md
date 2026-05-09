# SHUOSC_Network Server Backend

`SHUOSC_Network-server-backend` 是 SHUOSC_Network 的服务端后端仓库。
本 README 作为当前后端 Podman 部署文档，描述的是最终边界明确后的部署方式：

- Podman 容器只部署后端 API
- 管理面板和用户面板单独部署到 Cloudflare Pages
- 后端镜像默认不构建前端，不运行 npm，不携带 SPA 静态资源

## 仓库职责

这个仓库包含：

- `cmd/scnet-server`：服务端入口
- `cmd/scnet-relay-agent`：Cloudflare 反向 HTTP relay agent
- `internal/api`：HTTP API、认证中间件、路由
- `internal/account`：账户自助能力
- `internal/admin`：管理员能力
- `internal/peer`：WireGuard peer 管理与 IPAM
- `internal/discovery`：向 Cloudflare Pages `/api/server-info` 上报地址
- `internal/store`：PostgreSQL 持久化与迁移

## 部署边界

默认部署形态是 API-only。

- 后端通过 Podman 提供 `:8080` HTTP API 和 `:51820/udp` WireGuard
- 前端独立部署到 Cloudflare Pages
- 后端部署入口文件与源码同仓维护，位于本仓库根目录
- 只有在显式开启 `SCNET_ENABLE_SPA_SERVING=true` 时，后端才会尝试本地托管已存在的 SPA 构建产物
- WireGuard 公网入口可手动配置，也可显式启用内置 STUN punch/proxy 自动发现

如果你在构建日志里看到 npm/front-end build，说明你用的不是当前这套 backend-only 容器定义。

## 通信面分离规范

这套系统必须按 4 条独立通信面设计和部署：

| 通信面 | 来源 -> 目标 | 协议 | 用途 |
|--------|--------------|------|------|
| 前端访问面 | 浏览器 -> Cloudflare Pages | HTTPS | 访问用户/管理面板 |
| 后端控制面 | SPA/CLI -> 后端 API | HTTPS | 登录、鉴权、账户、peer、admin API |
| WireGuard 数据面 | 客户端 -> WireGuard 端点 | UDP | 隧道与真实业务流量 |
| 地址发现面 | SPA/CLI/后端 -> Cloudflare Pages `/api/server-info` | HTTPS | 发布和获取 `api_url`、`wg_endpoint` |

硬约束：

- `api_url` 必须是 HTTPS API 地址，例如 `https://api.example.com/api/v1`
- `wg_endpoint` 必须是 UDP 端点，例如 `wg.example.com:51820`
- Cloudflare Pages 只承载前端页面和 `/api/server-info`
- Cloudflare Pages 不承载 WireGuard UDP
- `api_url` 和 `wg_endpoint` 不能互相替代
- 即使 API 和 WG 最终落在同一台主机，也必须保持地址语义分离

推荐域名形态：

- `panel.example.com` -> Cloudflare Pages
- `api.example.com` -> HTTPS 反代到后端 `:8080`
- `wg.example.com:51820/udp` -> WireGuard
- `panel.example.com/api/server-info` -> 地址发现

## 部署兼容策略

兼容性最好的方式不是把后端改成适配某一种暴露手段，而是固定外部契约：

- `panel_url`：前端页面入口，HTTPS
- `discovery_url`：地址发现入口；当前前端默认使用同源 `/api/server-info`
- `api_url`：后端 REST API 入口，HTTPS
- `wg_endpoint`：WireGuard 入口，UDP `host:port`

在这个前提下：

- 公网直连统一部署：通常不需要改现有核心源码
- 公网直连分离部署：通常不需要改现有核心源码
- Lucky 正向暴露：通常不需要改现有核心源码
- 反向 HTTP relay：通常需要新增 relay/agent/worker 适配器，但不应默认要求改动 `scnet-server` 主体业务逻辑

结论：

- 写入部署规范本身，不需要改现有前后端业务源码
- 如果将来真正落地新的“反向 HTTP 暴露”能力，需要新增适配器实现
- 新增适配器不等于必须重写当前后端服务或前端业务逻辑

## 本仓库内的部署文件

以下文件都位于 `SHUOSC_Network-server-backend/`：

- `Containerfile`：后端镜像构建文件，只编译 Go 二进制并复制 migrations
- `podman-compose.yml`：本机 Podman 编排，包含 `server` 和 `db`
- `config.yaml`：容器内后端配置，使用环境变量注入敏感信息
- `.env.example`：环境变量模板，纳入 git 管理
- `.env`：本机部署环境变量文件，本地保留并由 git 忽略
- `.dockerignore`：限制构建上下文，避免 `.env` 进入镜像构建

## 前置要求

- Linux 主机，内核建议 `>= 5.6`
- `podman` 可用
- `podman compose` 或 `podman-compose` 可用
- 主机存在 `/dev/net/tun`
- 允许容器使用 `NET_ADMIN`
- 端口 `8080/tcp`、`51820/udp` 未被占用

## 需要准备的环境变量

在本仓库根目录创建 `.env`，至少包含：

```bash
cp .env.example .env
```

```dotenv
DB_PASSWORD=replace_with_postgres_password
SCNET_WG_PRIVATE_KEY=replace_with_wireguard_private_key
SCNET_JWT_SECRET=replace_with_long_random_secret
```

说明：

- `SCNET_WG_PRIVATE_KEY` 必须是 WireGuard 私钥（base64），可用 `wg genkey` 生成
- `SCNET_JWT_SECRET` 应使用足够长的随机值，例如 `openssl rand -base64 64`
- 空库第一次通过注册页创建的账号会自动成为管理员，且不需要邀请码
- 数据库已有用户后，注册必须提供管理员创建的邀请码
- `DB_PASSWORD` 必须与 `podman-compose.yml` 中 PostgreSQL 容器使用的密码一致
- `.env` 已被 `.gitignore` 忽略，不应提交到仓库

## 配置文件约定

本仓库内的 `config.yaml` 已按当前 Podman 部署方式接好变量引用：

- 数据库主机默认是 `db`
- WireGuard 接口默认是 `wg_scnet`
- 后端监听 `8080`
- WireGuard 监听 `51820`
- 敏感配置通过 `${...}` 从容器环境读取

如果你改动部署拓扑，优先同步修改 `config.yaml`，不要把密钥硬编码进仓库。

## 检查当前后端状态

本机后端 API 状态：

```bash
curl http://127.0.0.1:8080/health
```

预期：

```json
{"db":"connected","peers":0,"status":"ok","wg":"active"}
```

Cloudflare Worker 地址发现状态：

```bash
curl https://scnet-test.lonnet.uk/api/server-info
```

如果返回了 `api_url` 和 `wg_endpoint`，说明后端已经向 Worker 上报动态发现信息。
如果返回 `503 SERVER_INFO_UNAVAILABLE`，说明 Worker 在线，但后端还没有完成发现上报。

Cloudflare relay 当前是否接通后端：

```bash
curl https://scnet-test.lonnet.uk/health
```

- 返回 `200`：说明 relay agent 已接通后端
- 返回 `503 {"code":"RELAY_UNAVAILABLE"}`：说明 Worker 在线，但 relay agent 还没连上

## 启动后端 Podman 部署

从本仓库根目录执行：

```bash
podman compose -p shuosc_network -f podman-compose.yml --env-file .env up --build -d
```

如果你的环境使用独立的 `podman-compose` 命令，也可以：

```bash
podman-compose -p shuosc_network --env-file .env up --build -d
```

当前容器定义会做这些事：

1. 启动 PostgreSQL 容器
2. 构建后端镜像
3. 挂载 `config.yaml` 到 `/etc/scnet/config.yaml`
4. 注入数据库密码、WG 私钥、JWT 密钥、管理员密码
5. 以 `NET_ADMIN + /dev/net/tun` 拉起后端服务

说明：

- `podman-compose.yml` 将 PostgreSQL 数据卷固定为 `shuosc_network_pgdata`
- 这样从旧的工作区根部署迁移到本仓库内部署时，不会因为项目目录变化而丢失数据库卷
- 文档统一使用 `-p shuosc_network`，确保容器名、网络名、卷名在迁移前后保持稳定

如果你是从旧的工作区根目录部署迁移过来，建议先执行一次：

```bash
podman compose -p shuosc_network -f podman-compose.yml down
```

然后再按新的仓库内命令重新 `up --build -d`。

## 启动后验证

检查容器状态：

```bash
podman compose -p shuosc_network -f podman-compose.yml ps
```

查看健康检查：

```bash
curl http://127.0.0.1:8080/health
```

预期至少返回：

```json
{"db":"connected","peers":0,"status":"ok","wg":"active"}
```

查看服务日志：

```bash
podman compose -p shuosc_network -f podman-compose.yml logs server
```

正常启动日志应包含：

- `config loaded`
- `database connected`
- `migrations complete`
- `wireguard interface ready`
- `starting server addr=:8080`

## 连接后端到 scnet-test.lonnet.uk

当前 Cloudflare Worker 已经提供：

- `https://scnet-test.lonnet.uk/api/server-info`
- `https://scnet-test.lonnet.uk/_agent/connect`
- `https://scnet-test.lonnet.uk/_agent/status`

真正把本机后端 API 接到这个域名，需要运行 `scnet-relay-agent`，让它主动通过 WebSocket 连接到 Worker。

### 需要的 Cloudflare 运行时变量

在 Worker 面板中至少设置：

- `SCNET_AGENT_TOKEN`：必须，建议机密
- `SCNET_RELAY_TIMEOUT_MS=20000`
- `SCNET_RELAY_CHANNEL=default`

不要在 Worker 里把 `SCNET_WG_ENDPOINT` 填成 API URL。正常 relay 部署下，`wg_endpoint`
由后端启动后 POST 到 Worker 的 `/api/server-info`，Worker 只保存和返回最新上报值。

### 需要的本地环境变量

在本仓库 `.env` 中补充：

```dotenv
SCNET_AGENT_URL=https://scnet-test.lonnet.uk/_agent/connect
SCNET_AGENT_TOKEN=replace_with_same_cloudflare_secret
SCNET_AGENT_ID=shuosc-network-podman
SCNET_AGENT_BACKEND_URL=http://server:8080
SCNET_AGENT_PING_INTERVAL_MS=15000
SCNET_AGENT_REQUEST_TIMEOUT_MS=20000
SCNET_AGENT_RECONNECT_DELAY_MS=3000

SCNET_DISCOVERY_ENABLED=true
SCNET_DISCOVERY_URL=https://scnet-test.lonnet.uk/api/server-info
SCNET_DISCOVERY_API_ADDR=https://scnet-test.lonnet.uk/api/v1
SCNET_WG_ENDPOINT=replace_with_public_udp_endpoint_51820
```

说明：

- `SCNET_AGENT_TOKEN` 必须与 Cloudflare Worker 中的 `SCNET_AGENT_TOKEN` 完全一致
- `SCNET_AGENT_BACKEND_URL=http://server:8080` 是给容器内 relay-agent 使用的
- 如果你在宿主机直接运行 agent，则把 `SCNET_AGENT_BACKEND_URL` 改成 `http://127.0.0.1:8080`
- discovery 上报默认复用 `SCNET_AGENT_TOKEN`，不需要单独配置第二个 secret
- `SCNET_DISCOVERY_API_ADDR` 是浏览器/CLI 看到的 Cloudflare API 入口
- `SCNET_WG_ENDPOINT` 是客户端直连 WireGuard 的 UDP `host:port`，不是 HTTPS API URL

### WireGuard endpoint 发布方式

优先级固定如下：

1. 如果设置了 `SCNET_WG_ENDPOINT`，直接上报这个地址，不启用内置 STUN。
2. 如果未设置 `SCNET_WG_ENDPOINT` 且 `SCNET_PUNCH_ENABLED=true`，后端启动内置 UDP punch/proxy，通过配置的 STUN server 探测公网 `ip:port` 后上报。
3. 如果两者都没有，`SCNET_DISCOVERY_ENABLED=true` 会拒绝启动，避免把 `localhost:51820` 上报给 Worker。

手动公网地址适合有公网 IP、端口映射、Lucky 或其他外部转发器的部署：

```dotenv
SCNET_DISCOVERY_ENABLED=true
SCNET_WG_ENDPOINT=wg.example.com:51820
SCNET_PUNCH_ENABLED=false
```

内置 STUN punch/proxy 适合没有固定外部端口但 NAT 支持 UDP endpoint-independent mapping 的部署：

```dotenv
SCNET_DISCOVERY_ENABLED=true
SCNET_WG_ENDPOINT=
SCNET_PUNCH_ENABLED=true
SCNET_PUNCH_LISTEN_PORT=51280
SCNET_PUNCH_WG_HOST=127.0.0.1
SCNET_PUNCH_WG_PORT=51820
# 留空时使用 config.yaml 内置候选；填写后会覆盖内置列表
SCNET_STUN_SERVERS=
SCNET_PUNCH_PROBE_TIMEOUT_SECONDS=5
SCNET_PUNCH_KEEPALIVE_SECONDS=20
```

硬约束：

- `config.yaml` 内置的 `punch.stun_servers` 只放中国大陆方向候选，不内置 Google/Cloudflare 等国外公共 STUN
- `SCNET_STUN_SERVERS=host1:3478,host2:3478` 可在部署时覆盖内置列表
- `SCNET_PUNCH_LISTEN_PORT` 不能与内核 WireGuard 的本机监听端口相同
- `SCNET_PUNCH_LISTEN_PORT` 是容器内 punch/proxy socket，STUN 探测得到的公网端口可能不同；Worker 上报的是探测出来的公网 `ip:port`
- 内置 punch/proxy 会把外部 UDP 流量转发到本机 kernel WireGuard，不改变 WireGuard 协议
- STUN 不能穿透所有 NAT；对称 NAT、运营商 CGNAT 或 UDP 被封时仍需要公网转发或中继

### 方式 1：直接运行 agent（宿主机）

```bash
go run ./cmd/scnet-relay-agent \
  -url https://scnet-test.lonnet.uk/_agent/connect \
  -token "$SCNET_AGENT_TOKEN" \
  -backend http://127.0.0.1:8080
```

### 方式 2：Podman Compose 运行 agent（推荐）

`podman-compose.yml` 已新增可选 `relay-agent` 服务，使用 `relay` profile：

```bash
podman compose -p shuosc_network -f podman-compose.yml --env-file .env --profile relay up -d relay-agent
```

如果你的环境使用 `podman-compose` 命令：

```bash
podman-compose -p shuosc_network --env-file .env --profile relay up -d relay-agent
```

### 接通后的检查

检查 agent 状态：

```bash
curl -H "Authorization: Bearer $SCNET_AGENT_TOKEN" \
  https://scnet-test.lonnet.uk/_agent/status
```

接通后预期字段包括：

- `"connected": true`
- `"pending": 0`

检查 Cloudflare 域名是否已经真正回到本机后端：

```bash
curl https://scnet-test.lonnet.uk/health
curl https://scnet-test.lonnet.uk/version
```

这两个接口返回成功，说明 `scnet-test.lonnet.uk` 到本机后端的反向链路已经打通。

检查后端是否已经把 API 和 WireGuard 地址上报给 Worker：

```bash
curl https://scnet-test.lonnet.uk/api/server-info
```

预期形态：

```json
{
  "api_url": "https://scnet-test.lonnet.uk/api/v1",
  "wg_endpoint": "wg.example.com:51820",
  "updated_at": "2026-05-09T00:00:00Z"
}
```

## 常用运维命令

停止并保留数据卷：

```bash
podman compose -p shuosc_network -f podman-compose.yml down
```

停止并删除数据卷：

```bash
podman compose -p shuosc_network -f podman-compose.yml down -v
```

重建后端：

```bash
podman compose -p shuosc_network -f podman-compose.yml up --build -d server
```

## 可选：本地托管已构建的 SPA

这不是默认部署方式，也不属于 Podman 后端镜像职责。

只有在你明确要做本地联调时，才启用：

```bash
export SCNET_ENABLE_SPA_SERVING=true
export SCNET_SPA_DIR=/path/to/spa/dist
```

然后再启动后端进程。未开启时，后端对 `/app/*` 和 `/admin/*` 不提供 SPA。

## 常见问题

### 为什么现在构建时不该再有 npm？

因为当前 `Containerfile` 只构建 Go 后端二进制。前端管理面板已经从 Podman 后端部署链路剔除，改为独立部署到 Cloudflare Pages。

### 为什么这些部署文件要放在后端仓库里？

因为它们就是后端部署入口的一部分：

- `Containerfile` 决定后端镜像如何构建
- `podman-compose.yml` 决定后端和数据库如何拉起
- `config.yaml` 决定后端运行时配置
- `.env.example` 决定部署时需要哪些环境变量

这些文件和后端代码必须同仓维护，才能避免部署文件与服务端实现漂移。

### 如果容器启动后连不上数据库怎么办？

先检查：

- `podman-compose.yml` 中不要手动给 `server` 配 `network_mode: bridge`
- `podman compose -p shuosc_network -f podman-compose.yml ps` 里 `db` 是否为 `healthy`
- `config.yaml` 里的数据库主机是否仍是 `db`

### 如果 WireGuard 起不来怎么办？

优先检查：

- 主机是否存在 `/dev/net/tun`
- 容器是否拿到了 `NET_ADMIN`
- `SCNET_WG_PRIVATE_KEY` 是否为合法 WireGuard 私钥

## 开发命令

直接运行服务端：

```bash
go run ./cmd/scnet-server
```

运行测试：

```bash
go test ./...
go test -race ./...
go vet ./...
```
