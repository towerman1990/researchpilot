# 决策记录

| 状态 | 决策与原因 | 来源 |
| --- | --- | --- |
| 已实施 | 单 Agent、Go 自研运行时。API 和 Worker 分进程，当前 HTTP 使用 `net/http`。 | [README](../README.md)、[技术选型](../docs/06-TECH-STACK-DECISIONS.md) |
| 已实施 | MySQL 持久化任务、Outbox、检查点和 SSE 事件，Redis / Asynq 承担派发。以派发代次和租约 epoch 约束重复投递与过期执行器写入。 | [worker.go](../cmd/researchpilot/worker.go)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已实施 | 模型使用兼容 Chat Completions 的完整 JSON 路径，配置加密并保存不可变版本，任务绑定提交时选择的版本，以保证配置隔离与执行一致性。 | [model.go](../cmd/researchpilot/model.go)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已实施 | 使用增量迁移升级 M1，保留旧报告和未配置模型时的演示路径。迁移异常由人工检查。 | [迁移目录](../migrations)、[M2 实施记录](../docs/09-M2-IMPLEMENTATION.md) |
| 已采用，2026-11-18 | 以 Git 版本化交接摘要，技术设计继续由现有文档维护。同步前只在工作区干净时拉取，提交与推送由用户授权。 | [协作指南](../AGENTS.md) |
| 进行中 | Gin / Hertz 按原 ADR 待对照。MyGo 已确认是 egoist/mygo；MyGo v0.10.0 Windows 原型和安装器已构建，ADR 005 仍待真实安装及 Tauri 同用例验收。 | [技术选型](../docs/06-TECH-STACK-DECISIONS.md)、[桌面原型](../docs/13-M3-DESKTOP.md) |
| TODO | 根据实际验收确认预算、并发、事件留存阈值和搜索供应商配置。 | [M2 设计](../docs/08-M2-DESIGN.md) |
| 已采用，2026-11-18 | 使用 GitHub 托管的 Linux Actions runner 执行 Go/MySQL、前端和 Compose 烟测。使用临时测试凭据与主密钥，不使用真实模型密钥；部署另行建设。 | [CI 工作流](../.github/workflows/ci.yml) |
| 已实施，2026-11-18 | M2 使用有界依赖图和串行拓扑执行；每轮最多四步、最多三轮、默认 40k 总 Token / 五分钟。每逻辑模型调用最多三次尝试，跨恢复保持上限。 | [收尾记录](../docs/10-M2-CLOSEOUT.md) |
| 已实施，2026-11-18 | PARTIAL 仅整理已归档摘录，不伪造完整研究结论；事件默认保留终态后 30 天，报告和证据独立保留。 | [收尾记录](../docs/10-M2-CLOSEOUT.md) |
| 已实施，2026-11-18 | SSRF 策略继续拒绝 Fake-IP；提供可选 IP 字面量 HTTPS DNS，不改全局网络配置。DeepSeek JSON 调用关闭思考，最大输出 4096，其他兼容供应商不接收此厂商扩展。 | 真实联网故障与 [收尾记录](../docs/10-M2-CLOSEOUT.md) |
| 用户确认，2026-11-18 | 真实研究验收先使用指定官方 URL，不要求配置 Brave 搜索凭据。搜索适配器保留可选，真实搜索状态单独记录。 | 当前会话用户确认 |
| 已实施，2026-11-18 | 同一 URL 的重复片段不占满输入；主文档优先分配额度，Observe 根据归档证据判断目标覆盖，JSON 可修正一次。无支撑则 PARTIAL，不以来源数量冒充完成。 | [真实评估](../docs/11-M2-EVALUATION.md) |
