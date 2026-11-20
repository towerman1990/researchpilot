# API 与事件协议

| 方法和路径（前缀 /api/v1） | 契约 |
|---|---|
| POST /auth/register、/auth/login、/auth/refresh、/auth/logout | 注册登录、刷新轮换、撤销，Web 用安全 Cookie，桌面采用平台会话适配。 |
| GET / POST /conversations | 游标列表或创建会话。P0 用于任务归组。 |
| POST /research/tasks | body 含 goal、conversation_id、可选约束；Idempotency-Key 必需。返回 202、task_id 和状态 URL，异体同键 409。 |
| GET /research/tasks | 返回当前租户的任务数组，每页最多 50 条，按创建时间和任务 ID 降序排列。有更早的记录时，响应头 `X-Next-Cursor` 提供下一页的 `before` 值。 |
| GET /research/tasks/{id} | 获取任务快照，包括状态、阶段和最新事件序号。 |
| GET /research/tasks/{id}/events | SSE，按 Last-Event-ID 回放，也接受 after 用于初始连接。 |
| POST /research/tasks/{id}/cancel | 202 表示已受理；重复取消返回当前状态，其他终态冲突 409。 |
| GET /research/tasks/{id}/report、/research/tasks/{id}/sources/{source_id} | 获取报告及支撑证据；报告尚未生成时返回 409 和状态。 |
| GET / PUT /model-config、POST /model-config/probe | 读取脱敏配置、存新版本、探测能力。探测按租户限流。 |

登录失败使用通用错误；API 统一 code、message、request_id 和可选 retry_after。输入错误 422，未登录 401，跨租户实体 404，限流 429。列表使用稳定游标，时间为 ISO 8601 UTC。

## SSE 恢复与登录

事件采用 id = task 内 seq，event 为 task_status、plan_updated、tool_finished、report_ready 等，data 含 schema_version、task_id、seq、timestamp 与脱敏 payload。客户端先读快照再以其 seq 建流，按 seq 去重。服务端从 MySQL 读所有 seq 大于游标的事件并持续追尾，Redis 仅用作唤醒并补轮询以覆盖遗漏。游标过期发送 reset 并关闭，客户端重新读取快照后订阅。建议每 15 秒心跳，禁反代缓冲，配置读超时并逐次刷新输出。撤销后在下一轮校验或连接有效期结束时关闭流。

Web 使用同源会话 Cookie（Secure、HttpOnly、合适 SameSite），写操作验证 CSRF 与 Origin。桌面用系统浏览器和一次性绑定的登录回调（PKCE 与 state），短期访问令牌驻内存，刷新凭证存系统凭证库。桌面用可附 Authorization 的 fetch 流或等效受限原生桥，回放规则一致。配置明确允许的桌面来源，使用 HTTPS。令牌不上 URL，桌面壳限制外部导航，外链经系统浏览器打开。
