# API 与事件协议

| 方法和路径（前缀 /api/v1） | 契约 |
|---|---|
| POST /auth/register、/auth/login、/auth/refresh、/auth/logout | 注册登录、刷新轮换、撤销，Web 用安全 Cookie，桌面采用平台会话适配。 |
| GET / POST /conversations | 游标列表或创建会话。P0 用于任务归组。 |
| POST /research/tasks | 请求提供 `goal` 以及可选的 `parent_task_id`、`review_plan`、`review_citations`、`demo_knowledge`、`knowledge_library_id` 和 `attachments`，并携带 `Idempotency-Key`。返回 202、任务快照和 `Location` 响应头。相同幂等键携带不同请求内容时返回 409。 |
| GET /research/tasks | 返回当前租户的任务数组，每页最多 50 条，按创建时间和任务 ID 降序排列。有更早的记录时，响应头 `X-Next-Cursor` 提供下一页的 `before` 值。 |
| GET /research/tasks/{id} | 获取任务快照，包括状态、阶段和最新事件序号。 |
| GET /research/tasks/{id}/events | SSE，按 Last-Event-ID 回放，也接受 after 用于初始连接。 |
| POST /research/tasks/{id}/cancel | 202 表示已受理；重复取消返回当前状态，其他终态冲突 409。 |
| GET /research/tasks/{id}/report、/research/tasks/{id}/sources/{source_id} | 获取报告及支撑证据；报告尚未生成时返回 409 和状态。 |
| GET /research/tasks/{id}/report/evidence | 获取报告类型、归档来源数和按编号排列的引用。沿用报告的访问权限与状态码。 |
| GET / PUT /research/tasks/{id}/report/feedback | 读取或更新本账号对报告的反馈。评分为 `helpful` 或 `not_helpful`，备注最多 500 字。尚无反馈时读取返回空字符串。 |
| GET / PUT /model-config、POST /model-config/probe | 读取脱敏配置、存新版本、探测能力。探测按租户限流。 |

登录失败使用通用错误，API 错误响应提供 `code`。Hertz 为每个请求生成 `X-Log-ID`，访问日志记录编号、路由模板、方法、状态和耗时。任务快照的 `creation_logid` 可关联创建请求和 Worker 执行日志，任务重试和恢复继续使用原编号。输入错误 422，未登录 401，跨租户实体 404，限流 429。列表使用稳定游标，时间为 ISO 8601 UTC。

## 基于报告追问

`parent_task_id` 关联同租户已有报告的任务，报告可处于完成或部分完成状态。父任务不存在或属于其他租户时返回 404，存在但无可用报告时返回 409。任务详情和历史列表仅在存在父任务时返回该字段。追问沿用常规任务的模型配置、试用额度和队列。Worker 将上一份报告的有限摘录作为背景，新报告的引用仍来自当前任务归档的证据。


## 初始计划审批

创建任务时将 `review_plan` 设为 `true`，任务生成初始计划后进入 `AWAITING_APPROVAL`。详情中的 `review_plan` 是计划 JSON 字符串。向 `POST /research/tasks/{id}/plan/approve` 提交快照中的字符串 `event_seq` 和编辑后的 `plan` 对象。有效审批返回 202 并生成新的派发记录。状态或序号过期返回 409，非法计划返回 422。系统在审批后启动五分钟执行时限，后续补充取证按既有规则自动进行。

## 研究偏好

`GET /research/preferences` 读取当前账号的偏好，`PUT /research/preferences` 保存或替换。字段为 `sources`、`detail` 和 `structure`。来源支持 `official` 和 `github`，详略支持 `brief` 和 `detailed`，结构支持 `conclusion_first` 和 `bullets`。空值表示系统默认，提交空对象可清除已有偏好。非法值返回 422。

新建任务和追问会保存当时的偏好快照，重试和计划审批恢复沿用该快照。Worker 将偏好作为默认要求，并优先遵循任务中用户明确提出的要求。

## 多套模型配置

`GET /model-config` 提供当前配置的兼容字段和脱敏的 `profiles` 列表。`PUT /model-config` 更新当前配置，或通过 `new_profile: true` 新建一套配置并设为当前。配置名称为可选 `label`。编辑时可提供 `expected_version`，当前配置变化后返回 409。新增配置或更换 Base URL 需要填写该配置的 API Key。

`POST /model-config/activate` 接收 `profile_id`，将同账号下有效的配置设为当前。切换及编辑只影响之后创建的任务。历史任务保存的凭据版本在显式撤销前仍可用于执行与恢复。API 响应只返回脱敏信息。

## 上传研究资料

将 `demo_knowledge` 设为 `true` 可为当前任务启用公开知识库检索，需有个人模型或平台试用模型。该选项参与请求幂等校验，命中内容归档为任务证据。与 `review_plan` 同时启用时，在计划批准后检索。

已登录用户可通过 `GET/POST /api/v1/knowledge/libraries` 列出或创建资料库（创建请求含 `name`），并通过 `GET/POST /api/v1/knowledge/libraries/{id}/documents` 查看文档版本或上传 `name`、`content`。上传返回 202 和 `QUEUED` 状态，索引完成后状态变为 `READY`。创建研究任务时传入 `knowledge_library_id` 可选用当前账号的资料库；该字段参与幂等校验。访问他人资料库返回 404。

创建任务时可传入 `attachments` 数组，每项包含 `name` 与 UTF-8 `content`。支持 TXT 与 Markdown，限两份，每份最多 24 KiB。非法输入返回 422。资料与任务在同一事务中保存，并通过当前任务的授权来源接口读取。资料摘录作为模型证据，报告可引用原文件名。

## 研究用量

`GET /research/usage?days=7` 返回当前账号最近 7 个 UTC 自然日的用量，包含当天。可选 `days=30`，其他值返回 422。结果按日期升序排列，无用量的日期返回零值。

每日字段为 `day`、`actual_tokens`、`estimated_tokens`、`attempts` 和 `failed_attempts`。按调用结算时间统计，包含实际和估算结算，失败数来自已记录错误码的调用。供应商费用以账单为准。

`GET /research/usage/models` 使用相同的时间范围，按个人模型地址与名称返回 `input_tokens`、`output_tokens`、`unsplit_tokens` 和 `estimated_tokens`。输入输出分项需要由上游同时提供并与总量一致。站内试用调用仍计入通用用量面板。

## 多任务概览

`GET /research/overview` 返回当前账号的 `open`、`finished` 和 `more_open`。未结束任务按创建时间倒序，最多 50 条，超过时 `more_open` 为真。已结束任务按结束时间倒序，最多 10 条。

条目包含任务 ID、研究目标、状态和 UTC 创建时间，并在有值时返回执行阶段与结束时间。工作区可见时定时刷新，单个任务仍可在详情页通过 SSE 实时跟踪。

## 研究评估

`GET /research/evaluation?days=7` 返回本账号真实研究的运行结果、报告与反馈计数，以及最近 12 项可复查的任务。可选 `days=30`。以任务结束时间划分 UTC 自然日，演示任务不计入。

`cited_reports` 是至少有一条归档引用的报告数。反馈取报告当前的评分，复查列表包含部分完成、失败，以及已完成但被标记为 `not_helpful` 的任务。

## 定时研究

`GET /research/schedules` 列出当前账号的研究计划，活动计划优先，最多 100 条。`POST /research/schedules` 接受 `goal`、RFC 3339 格式的 `first_run` 和 `interval_days`。间隔为 0、1 或 7，分别表示单次、每天和每周。`POST /research/schedules/{id}/cancel` 取消仍活动的计划。

创建和触发都使用当前账号的个人模型配置。触发时没有可用配置则标为 `blocked`。Worker 在同一事务中创建任务、写入 Outbox 并推进计划。停机跨过多个周期时只补建一次，下一次运行回到原间隔。响应中的 `last_task_id` 可用于查看最近一次触发的任务。

创建时可传 `notify_results: true`。活动计划可通过 `POST /research/schedules/{id}/subscription` 更新此字段。订阅以任务触发时的计划设置为准。

`GET /research/schedule-results` 返回本账号已结束的订阅任务，未读优先，每组按结束时间倒序，最多 50 条。`POST /research/schedule-results/{task_id}/read` 将结果标为已读，重复调用返回 204。

## SSE 恢复与登录

事件采用 id = task 内 seq，event 为 task_status、plan_updated、tool_finished、report_ready 等，data 含 schema_version、task_id、seq、timestamp 与脱敏 payload。客户端先读快照再以其 seq 建流，按 seq 去重。服务端从 MySQL 读所有 seq 大于游标的事件并持续追尾，Redis 仅用作唤醒并补轮询以覆盖遗漏。游标过期发送 reset 并关闭，客户端重新读取快照后订阅。建议每 15 秒心跳，禁反代缓冲，配置读超时并逐次刷新输出。撤销后在下一轮校验或连接有效期结束时关闭流。

Web 使用同源会话 Cookie（Secure、HttpOnly、合适 SameSite），写操作验证 CSRF 与 Origin。桌面用系统浏览器和一次性绑定的登录回调（PKCE 与 state），短期访问令牌驻内存，刷新凭证存系统凭证库。桌面用可附 Authorization 的 fetch 流或等效受限原生桥，回放规则一致。配置明确允许的桌面来源，使用 HTTPS。令牌不上 URL，桌面壳限制外部导航，外链经系统浏览器打开。
