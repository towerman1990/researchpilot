# 决策记录

| 状态 | 决策与原因 | 来源 |
| --- | --- | --- |
| 已实施 | 单 Agent、Go 自研运行时。API 和 Worker 分进程，当前 HTTP 使用 `net/http`。 | [README](../README.md)、[技术选型](../docs/06-TECH-STACK-DECISIONS.md) |
| 已实施 | MySQL 持久化任务、Outbox、检查点和 SSE 事件，Redis / Asynq 承担派发。以派发代次和租约 epoch 约束重复投递与过期执行器写入。 | [worker.go](../cmd/researchpilot/worker.go)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已实施 | 模型使用兼容 Chat Completions 的完整 JSON 路径，配置加密并保存不可变版本，任务绑定提交时选择的版本，以保证配置隔离与执行一致性。 | [model.go](../cmd/researchpilot/model.go)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已实施 | 使用增量迁移升级 M1，保留旧报告和未配置模型时的演示路径。迁移异常由人工检查。 | [迁移目录](../migrations)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已采用，2026-11-18 | 以 Git 版本化交接摘要，技术设计继续由现有文档维护。同步前只在工作区干净时拉取，提交与推送由用户授权。 | [协作指南](../AGENTS.md) |
| TODO | 对照验证 Gin 和 Hertz 后再定 HTTP 框架。确认桌面壳及 MyGo 指代，并按双端发布验收选型。 | [技术选型](../docs/06-TECH-STACK-DECISIONS.md) |
| TODO | 根据实际验收确认预算、并发、事件留存阈值和搜索供应商配置。 | [M2 设计](../docs/08-M2-DESIGN.md) |
| 已采用，2026-11-18 | 使用 GitHub 托管的 Linux Actions runner 执行 Go/MySQL、前端和 Compose 烟测。使用临时测试凭据与主密钥，不使用真实模型密钥；部署另行建设。 | [CI 工作流](../.github/workflows/ci.yml) |
