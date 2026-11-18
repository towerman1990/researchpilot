# 当前架构

- **入口与部署。** [main.go](../cmd/researchpilot/main.go) 提供 API、Worker 和迁移入口。Compose 使用 MySQL 8.4 和 Redis 7.4，数据库健康检查通过后运行迁移，迁移成功后启动应用。API 在本地映射到 `127.0.0.1:8080`。Provider、计划与工具契约位于 internal/model、internal/agent、internal/tools。详见 [compose.yaml](../compose.yaml)。
- **认证与接口。** Go `net/http` API 提供同源会话、任务创建和查询、取消、报告、来源、模型配置及 SSE。业务访问按认证租户过滤，任务创建使用幂等键。实际路由见 [api.go](../cmd/researchpilot/api.go)。
- **派发与执行。** API 原子写入任务、初始事件、幂等键和 MySQL Outbox。Worker 转发 Outbox 到 Redis / Asynq，按派发代次领取任务，以租约及 epoch 约束写入。检查点支持恢复，取消和重试由数据库状态协调。参见 [worker.go](../cmd/researchpilot/worker.go) 和 [execution.go](../cmd/researchpilot/execution.go)。
- **研究与证据。** Worker 通过兼容 Chat Completions 的完整 JSON 响应生成计划和报告。模型配置分租户加密，任务绑定配置版本。只读工具支持公网页面、公开 GitHub 信息和可选 Brave 搜索，来源先持久化，报告引用限定在当前任务。参见 [research.go](../cmd/researchpilot/research.go)、[model.go](../cmd/researchpilot/model.go)、[tools.go](../cmd/researchpilot/tools.go) 和 [outbound.go](../cmd/researchpilot/outbound.go)。
- **事件与存储。** MySQL 是任务状态、报告和事件的事实来源。SSE 支持事件游标回放，React 按 seq 去重并归并工具事件。实际表和索引以 [SQL 迁移](../migrations) 为准，前端归并见 [events.js](../web/events.js)。

M2 收尾已加入流式 Provider 契约、步骤依赖校验、事件过期快照恢复、PARTIAL、配置探测与撤销、密钥版本和 Redis 限流。API 启动要求 006 迁移。运行时持久层仍在 cmd 组装，后续拆分与生产可观测性、反向代理验收边界见 [M2 收尾记录](../docs/10-M2-CLOSEOUT.md)。
