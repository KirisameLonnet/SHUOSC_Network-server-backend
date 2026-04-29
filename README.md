# SHUOSC_Network Server Backend

`SHUOSC_Network-server-backend` 是 SHUOSC_Network 的服务端后端仓库。
本 README 作为当前后端 Podman 部署文档，描述的是最终边界明确后的部署方式：

- Podman 容器只部署后端 API
- 管理面板和用户面板单独部署到 Cloudflare Pages
- 后端镜像默认不构建前端，不运行 npm，不携带 SPA 静态资源

## 仓库职责

这个仓库包含：

- `cmd/scnet-server`：服务端入口
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
ADMIN_PASSWORD=replace_with_bootstrap_admin_password
```

说明：

- `SCNET_WG_PRIVATE_KEY` 必须是 WireGuard 私钥（base64），可用 `wg genkey` 生成
- `SCNET_JWT_SECRET` 应使用足够长的随机值，例如 `openssl rand -base64 64`
- `ADMIN_PASSWORD` 仅用于首次空库启动时自举 `admin` 账户
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
- `admin user bootstrapped`（仅空库首次启动）
- `wireguard interface ready`
- `starting server addr=:8080`

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
