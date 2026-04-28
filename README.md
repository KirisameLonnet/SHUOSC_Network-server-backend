# SHUOSC_Network Server Backend

`SHUOSC_Network-server-backend` 是 SHUOSC_Network 的服务端后端仓库。

这个仓库包含服务端核心实现：

- `cmd/scnet-server`：服务端入口
- `internal/api`：HTTP API、认证中间件和路由
- `internal/account`：账户、邀请和管理员相关业务逻辑
- `internal/peer`：WireGuard peer 管理
- `internal/store`：数据库访问和持久化
- `web`：嵌入式前端静态资源

## Development

运行服务端：

```bash
go run ./cmd/scnet-server
```

运行测试：

```bash
go test ./...
```
