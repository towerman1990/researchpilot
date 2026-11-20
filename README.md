# ResearchPilot

ResearchPilot 是一个本地技术研究工作台。你可以注册、登录、提交研究目标、查看任务历史，并通过 SSE 实时跟踪进度。M1 演示路径可用。M2 已实现模型驱动的研究、证据引用、任务取消和恢复重试，完整验收仍在进行。

## 启动

Windows 用户需要 PowerShell 7、Docker 和 Compose。在仓库根目录运行以下命令。升级已有数据库前先备份 MySQL。macOS 和 Linux 用户请参阅 [macOS / Linux 部署指南](docs/14-MACOS-LINUX.md)，无需安装 PowerShell 即可启动。

```powershell
pwsh -File ./scripts/init-local.ps1
docker compose up --build -d
docker compose ps
```

初始化脚本在缺少密钥时生成 `.cache/model-master.key`。请保留此文件，以便解密已保存的模型配置。首次创建 MySQL 卷时，镜像执行 `migrations/001_m1.sql`。Compose 的 `migrate` 服务应用增量迁移，成功后启动 API 和 Worker。

启动后，用浏览器访问 [http://localhost:8080](http://localhost:8080)。API 仅映射到本机回环地址，Compose 中的数据库凭据用于本地开发。

## 运行研究任务

登录后，在工作台的「模型设置」中填写供应商 Base URL、模型名称和 API Key。保存后，新任务会使用该配置。未配置模型时，任务走 M1 演示路径。

真实研究可读取公网页面和公开 GitHub 仓库信息。启用 Brave 搜索需要在本地 Compose 环境中设置 `BRAVE_SEARCH_API_KEY`。未启用搜索时，可以在研究目标中提供公开网页 URL 或公开 GitHub 仓库链接。

## 演示烟测

服务启动后，可运行以下命令验证 M1 演示闭环。脚本会创建两个测试账号和一个任务，检查幂等请求、租户隔离、SSE 回放及演示报告。

```powershell
pwsh -File ./scripts/smoke.ps1
```

## 主链路

1. API 事务写入任务、幂等键、初始事件和 Outbox。
2. Worker 进程中的转发循环认领 Outbox，通过 Asynq 向 Redis 入队。
3. Worker 校验派发代次并取得租约。研究任务由模型规划步骤，通过只读工具收集并保存证据，生成经引用校验的报告。演示任务使用固定工具和模型适配器。
4. Worker 保存执行检查点与事件，并原子提交最终报告和任务终态。API 从 MySQL 回放 SSE 事件，支持用 `Last-Event-ID` 续传。
5. Worker 处理任务取消，回收循环处理过期租约、已派发但长时间未领取的任务，以及达到截止时间的任务。恢复执行可复用已提交的检查点。

## 项目现状

- 程序入口在 `cmd/researchpilot`，依赖由 `internal/wiring` 装配，`internal/app` 管理进程启动和关闭。
- `internal/handler` 提供 Hertz API，`internal/task` 管理任务，`internal/execution` 运行研究流程并通过 Eino 编排各阶段。任务执行的 MySQL 查询与事务由 `internal/repository` 管理，网页抓取、搜索和 GitHub 工具实现在 `internal/tools/research`。API 与 Worker 分进程运行。
- M1 迁移使用 32 字符的随机十六进制 ID，每个用户独占一个个人租户。身份使用 bcrypt 与 8 小时的 HttpOnly 会话 Cookie。Web 与 API 同源。
- 真实模型十题评估、完整流式能力探测、限流、事件过期恢复与清理、桌面客户端仍待完成。当前配置面向本地开发。

## 本地构建

需要 Go 1.25+、Node.js 22.12+ 和 npm。Go 与前端依赖均已锁定。

```powershell
go test ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
```

也可分别运行 `go run ./cmd/researchpilot api` 和 `go run ./cmd/researchpilot worker`。先在 MySQL 应用迁移并设置 `MYSQL_DSN`，Worker 的 `REDIS_ADDR` 默认为 `127.0.0.1:6379`。API 的 `PUBLIC_ORIGIN` 默认为 `http://localhost:8080`，须和浏览器访问地址一致。

## 文档与协作

- [技术文档索引](docs/README.md)
- [M2 设计](docs/08-M2-DESIGN.md)与 [M2 实施和验证记录](docs/09-M2-IMPLEMENTATION.md)
- [Codex 跨设备协作指南](AGENTS.md)

## 持续集成

[CI 工作流](.github/workflows/ci.yml) 在推送 main、提交 PR 和手动触发时运行：

- Go 格式检查、模块校验、vet、竞态检测，以及 MySQL 8.4 迁移和持久执行集成测试。
- Node.js 22 下安装锁定依赖、前端事件测试和生产构建。
- 构建 Docker 镜像并启动完整 Compose 栈，运行 M1 烟测，验证任务流转、租户隔离、幂等和 SSE；失败时输出容器诊断。

CI 使用临时数据库和随机生成的模型加密主密钥，不需要仓库 Secrets，也不访问真实模型供应商。真实模型质量评估仍需独立人工验收。CI 不发布镜像、不部署服务器。工作流推送至 GitHub 后才会开始云端运行；如需禁止合并失败的 PR，请在 GitHub 分支规则中将三个 CI 检查设为必需检查。

M2 新增配置探测、撤销、主密钥轮换、PARTIAL 报告、事件过期恢复和 Redis 限流。迁移、验收和十题真实评估操作见 [M2 收尾与放行记录](docs/10-M2-CLOSEOUT.md)。

M2 最终固定十题完成 8/10，并进行了助手关键证据复核；结果、失败案例及用户终验边界见 [真实评估记录](docs/11-M2-EVALUATION.md)。
