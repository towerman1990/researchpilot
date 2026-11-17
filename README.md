# ResearchPilot M1

在本地运行研究任务最小闭环。Web 可以注册、登录、提交研究目标、查看历史、实时接收 SSE 事件，并读取 Worker 保存的演示报告。固定工具与模型适配器用于验证数据流。

## 启动

需要 Docker 和 Compose v2。进入本目录后运行。

```powershell
docker compose up --build -d
docker compose ps
```

用浏览器访问 http://localhost:8080。API 只映射到本机回环地址。Compose 中的账号和密码为本地开发默认值。完整启动后可执行只读与写入混合的烟测，测试会创建两个账号和一个任务。

```powershell
pwsh -File ./scripts/smoke.ps1
```

首次创建 MySQL 卷时，镜像自动运行 migrations/001_m1.sql。已有数据库须明确执行迁移。mysql-data 与 redis-data 是独立持久卷。

## 主链路

1. API 事务写入任务、幂等键、初始事件和 Outbox。
2. Worker 进程中的转发循环认领 Outbox，通过 Asynq 向 Redis 入队。
3. 执行器校验派发代次，领取任务并保存运行事件。固定工具生成一条演示结果，固定模型适配器生成明确标记为演示的 Markdown。
4. Worker 在持有效租约时原子提交工具事件、报告与终态。API 从 MySQL 回放 SSE 事件，以 Last-Event-ID 续传。
5. 回收循环处理过期租约、已派发却长时间未被领取的任务，以及达到截止时间的任务。

## 本轮决策

- API 使用 Go 标准库 net/http 搭建，保持 Gin 与 Hertz 的 ADR 开放。API 与 Worker 作为不同进程运行。
- M1 迁移使用 32 字符的随机十六进制 ID，每个用户独占一个个人租户。身份使用 bcrypt 与 8 小时的 HttpOnly 会话 Cookie。Web 与 API 同源。
- 首版事件表保留全部演示数据。真实模型和联网搜索、证据引用、细粒度重试、取消、限流、事件清理、桌面客户端属于后续实施。当前阶段只用于本地开发，公网部署需先补齐认证与限流等控制。

## 本地构建

需要 Go 1.25+、Node.js 22.12+ 和 npm。Go 与前端依赖均已锁定。

```powershell
go test ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
```

也可分别运行 `go run ./cmd/researchpilot api` 和 `go run ./cmd/researchpilot worker`。先在 MySQL 应用迁移并设置 `MYSQL_DSN`，Worker 的 `REDIS_ADDR` 默认为 `127.0.0.1:6379`。API 的 `PUBLIC_ORIGIN` 默认为 `http://localhost:8080`，须和浏览器访问地址一致。
