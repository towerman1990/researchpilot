# ResearchPilot M2 研究与恢复详细设计

历史详细设计稿 · 2026-11-18 · 基于已交付 M1 制定。本文提出 M2 实施契约，具体预算和供应商配置仍需验证。版本化来源为本文件，飞书副本同步时记录对应提交。

实际实现、验证结果和保留差异见 [M2 收尾与放行记录](10-M2-CLOSEOUT.md)。本文的历史待实施说明不代表当前进度。

## 目标与范围

用户提交技术研究目标，系统自动规划，调用网络搜索、公网页面读取和公开 GitHub 分析，形成带证据引用的报告。Worker 重启后复用已提交结果，用户可取消，前端能从事件恢复进度。沿用单 Agent、自研 Go Runtime、React、MySQL、Redis、Asynq 和 SSE。

M2 分五个交付单元。人工审批、长期记忆、上传资料、多 Agent、MCP、深度仓库分析及桌面打包沿用后续阶段。模型接入使用 OpenAI-compatible Chat Completions，服务端校验结构化输出。HTTP 框架 ADR 保持待定。

| 单元 | 交付内容 | 放行条件 |
|---|---|---|
| M2.1 | 增量迁移、工具调用记录、租约续期、取消与预算 | 用工具桩通过重复派发及恢复测试 |
| M2.2 | ModelProvider、加密配置、能力探测 | 流中断、429、工具参数片段等契约测试通过 |
| M2.3 | 三个只读工具、证据归档及出站策略 | 公网夹具与 SSRF 测试通过 |
| M2.4 | Plan Execute Observe Replan、报告与引用校验 | 固定模型桩跑通全闭环及故障恢复 |
| M2.5 | 事件归并、真实模型评估、部署验收 | 经 Compose 运行的 10 题评估及故障用例通过 |

## M2 更新内容

| 模块 | 更新内容 |
|---|---|
| 工具注册与执行 | 启动时建立只读工具注册表，集中管理三个工具适配器及资源生命周期；使用结构化 ToolError 区分可修正、可重试和终止错误 |
| 模型调用 | 接入真实 OpenAI-compatible 模型，持久化调用尝试，明确流式输出及传输失败的重试边界 |
| 前端事件 | 通过纯 reducer 按 seq 去重，以稳定的逻辑调用 ID 关联工具调用、尝试及结果 |
| 任务恢复 | 持久化计划、步骤和调用结果，结合租约代次恢复执行；为 P1 计划审批保留版本校验 |
| 模型配置 | 增加能力探测、密钥加密、脱敏返回及 Base URL 出站校验 |

运行 Context 来自 Worker 与数据库截止时间，SSE 断开只影响订阅。M2 定义 Tool 与 Provider 接口，外部 SDK 通过适配器接入。

## 模块边界

将 cmd/researchpilot 保留为入口及依赖组装，把业务逐步移入 internal/task、internal/agent、internal/model、internal/tools、internal/evidence 和 internal/events。先搬迁现有逻辑并保持接口行为，再增加新能力，避免同时重写 HTTP 和任务持久层。

~~~mermaid
flowchart LR
  UI[React 工作台] --> API[任务与配置 API]
  API --> DB[(MySQL)]
  DB --> Relay[Outbox Relay]
  Relay --> Queue[Redis Asynq]
  Queue --> Worker[Worker 租约与恢复]
  Worker --> Runtime[Plan Execute Observe Replan]
  Runtime --> Provider[ModelProvider]
  Runtime --> Registry[Tool Registry]
  Registry --> Web[搜索 页面 GitHub]
  Worker --> DB
  DB --> SSE[SSE 事件读取]
  SSE --> UI
~~~

## Runtime 与调用协议

计划包含 plan_version、goal、constraints、steps。每个 step 包含稳定 step_id、研究问题、依赖步骤、候选工具和成功条件。服务端验证依赖无环、步数有界、工具已注册。计划变更保存新版本，执行中的单个步骤串行推进，M2 默认每任务同时运行一个工具。

每次逻辑调用由应用生成 tool_call_id，绑定 task_id、plan_version、step_id、call_no。供应商的 tool call ID 另存 provider_call_id，用于构造模型历史。参数规范化摘要用于审计与缓存，调用身份由稳定 ID 决定。尝试编号 attempt_no 从 1 递增，因传输问题重试沿用逻辑 ID，修正参数形成新逻辑调用。

1. Plan 在任务锁与有效 epoch 内保存计划、阶段及 plan_updated 事件。
2. Execute 验证工具名、参数 schema、租户策略、时间及预算，持久化调用 STARTED 后才发出网络请求。
3. 结果返回后，在同一事务里提交成功尝试、证据、步骤检查点和 tool_finished 事件。所有提交都重新检查 epoch、状态、租约和截止时间。
4. Observe 从持久证据重建覆盖矩阵，决定继续、重规划或收口。将短理由作为事件保存。
5. Replan 保存新版本，旧的调用与证据可追查；明确记录复用的证据 ID。
6. Synthesize 输出结构化章节和引用 ID，后端校验并生成 Markdown。报告、引用及终态同事务提交。

恢复时读取已提交计划和调用结果。STARTED 遗留尝试标记 ABANDONED，创建新 attempt；重复远程调用可能再次计费。SUCCEEDED 逻辑调用直接复用，不重复执行业务请求。Worker 对数据库写入被拒后取消执行 Context，丢弃迟到结果。

## ModelProvider 与兼容性

统一 Generate(ctx, request) 和 Stream(ctx, request) 契约。Request 包含 messages、tools、response schema、最大输出和请求幂等标识。Response 包含文本、tool calls、usage、finish_reason、provider_request_id。Stream 归一化文本增量、工具调用片段、usage 与结束事件。

配置存 provider 名称、HTTPS Base URL、model、超时、上下文上限和 capabilities。第一版支持单活跃配置及不可变版本，任务引用提交时的版本。探测分别覆盖基础对话、工具调用、流式工具调用和结构化输出。缺少原生 JSON Schema 时可采用 JSON 输出加服务端校验，最多修正一次，修正消耗预算。

工具调用片段按 index 与供应商调用 ID 合并，只有收到完整结束标记、合法 finish_reason 且 JSON 完整后才执行。参数、最终调用名由服务端校验。上游断流时保存失败 attempt，不能把半截参数交给工具。

HTTP 408、429、500、502、503、504 和可恢复网络错误可限次重试，401、403 及参数错误直接反馈配置或任务。已向 Runtime 提交内容时不在适配器内部静默重放。Runtime 如需重试，开新持久 attempt，丢弃前一次未提交输出，前端收到 replacement/attempt 事件。默认每逻辑模型调用最多 2 次额外尝试，退避加抖动并遵从 Retry-After 和任务剩余时间。

密钥采用 AES-GCM 信封加密，存 key_version、nonce 和 ciphertext，AAD 绑定 tenant/config/version。主密钥通过受限文件或密钥服务提供。探测和真实调用采用同一出站策略，禁跟随携带 Authorization 的重定向。配置读取返回 key_configured 与末尾掩码。任务快照及事件只包含配置 ID 和版本。

## 工具及证据契约

ToolSpec 包含 name、version、input_schema、timeout 和最大输出字节。ToolResult 包含 evidence、summary、truncated、retry_after 和 error。错误分 invalid_arguments、rate_limited、upstream_unavailable、policy_denied、cancelled、budget_exhausted。参数错误允许模型修正一次；策略拒绝、取消及预算耗尽由 Runtime 处理。日志与模型反馈均使用脱敏摘要。

| 工具 | 输入 | 输出及限制初值 |
|---|---|---|
| web_search | query、limit | 每次最多 5 条，标题、URL、摘要、provider、检索时间。供应商适配器可替换，摘要作为发现线索 |
| fetch_page | url | HTTP(S) HTML/文本，超时 15 秒，最多 3 次重定向，响应及解压后最大 2 MiB，正文最多 30k 字符 |
| github_repo | owner、repo、可选 ref | 仅公开仓库，固定 commit SHA，元数据、README、一级目录、至多 5 个许可的依赖清单，每文件最多 256 KiB |

GitHub API 路径由字段构造，owner/repo/ref 做格式校验及编码。处理 rate-limit 与 Retry-After。M2 读取允许列表内文件，遇到 symlink/submodule 跳过并提示。来源链接绑定 commit，避免分支变动改变证据。

所有工具流量经过统一出站客户端，验证 DNS 所有结果、绑定经验证 IP 并保留 Host/SNI。每次重定向重新验证，拒绝环回、私网、链路本地、保留地址和云元数据目标。超时、响应大小和解压上限同时实施。真实模型只收到必要的证据片段，来源内容作为不可信数据，不能扩展工具权限。

每条证据保存 source_id、canonical_url、title、excerpt、content_hash、retrieved_at、source_kind、commit_sha 和定位信息。搜索摘要若未读原文须明确标为摘要证据，不能支撑关键技术结论。fingerprint 按来源 URL、版本及内容摘要去重，限定同一任务。

报告模型只能引用当前任务实际保存的 Source ID。最终正文使用服务器生成的编号及 URL。每个关键结论需映射一条或多条证据，后端验证归属、存在性及定位；语义是否支撑结论由评估与人工抽查验证。缺失引用先修正一次，仍无支撑的结论移除或标记缺口。语义质量门槛不以 ID 校验代替。

## 持久化增量设计

沿用 M1 CHAR(32) ascii_bin ID、个人 tenant_id、task_events 和 Outbox。详细设计中的表名为逻辑契约，M2.1 产出并实际执行迁移后才冻结 SQL。

| 增量对象 | 核心字段与约束 |
|---|---|
| research_tasks | 增加 runtime_version、plan_version、retry_at、retry_count、max_steps、max_replans、token_limit、tokens_reserved、tokens_used、cancel_requested_at、finished_at。扩展状态及 phase。旧数据 runtime_version 回填 m1 |
| model_configs / model_config_versions | 个人租户配置 ID 与不可变版本，模型参数、能力、密钥密文及版本、撤销标记。任务复合 FK 指向租户内版本 |
| task_plans | PK tenant/task/plan_version，validated plan JSON、创建 epoch 和时间 |
| task_steps | tenant/task/plan_version/step_id 唯一，顺序、状态、成功条件及结果证据映射 |
| tool_calls | 稳定逻辑 ID、步骤、工具版本、规范参数和 input hash，步骤内 call_no 唯一 |
| tool_attempts | tenant/task/tool_call_id/attempt_no 唯一，epoch、开始结束、状态、脱敏错误和输出摘要 |
| research_sources | 同任务 source_id 唯一，fingerprint 唯一，定位与片段不可变 |
| model_attempts | 调用/attempt ID、配置版本、epoch、预留量、实际量、结算状态及供应商请求 ID |
| report_citations | tenant/task/report_revision/ordinal 唯一，复合 FK 指向同任务证据 |
| research_reports | 保留 M1 markdown，增加 schema_version、kind 和发布版本。旧报告 kind=demo，M2 kind=research |
| request_keys | 扩展 operation、规范请求摘要和 24 小时过期策略。旧幂等记录保持可读直到按新保留策略回收 |

所有租户实体之间用复合唯一键与外键。时间统一 UTC DATETIME(6)，恢复使用数据库时钟。task_steps 与 attempts 增加状态扫描索引；task_events 保持任务内 seq 唯一；来源在报告仍存续时不能单独硬删除。

采用 expand migrate contract。先部署能读旧新字段的版本，再执行回填与新 Worker。已开始的 m1 任务使用兼容演示路径，已有 m1 报告原样读取。新研究任务必须有有效模型配置。旧 API 创建请求只含 goal 时仍可用；响应和事件新增字段保持可选。M2 不直接覆盖 001_m1.sql，新增 002 迁移，历史卷由显式迁移命令升级。迁移时暂停领任务、备份并保留旧字段，不自动清空卷。

## 租约 取消 重试与预算

任务状态为 QUEUED、RUNNING、RETRY_WAIT、CANCELLING、SUCCEEDED、PARTIAL、FAILED、CANCELLED。phase 为 planning、executing、observing、replanning、synthesizing。对外状态、检查点和事件在同一任务锁事务内提交。

租约初值 30 秒，每 10 秒续期，同时检查取消标记、截止时间和 epoch。续期失败立即取消模型和工具请求。取消写入 CANCELLING 后，所有完成提交必须失败。若 Worker 未确认，由回收器在租约结束后写 CANCELLED。前端尽快显示“取消中”；活 Worker 的取消收口目标 10 秒，Worker 失联时收口上限为剩余租约加扫描周期，当前初值至多约 35 秒。

可恢复的任务级错误写 RETRY_WAIT、retry_at、原因和检查点，释放租约。到期调度器生成新 generation 与 Outbox。最多 2 次任务恢复重试，并受总 deadline 约束。队列重试只弥补基础设施层失败，领取后执行事实由 MySQL 管理。旧 generation 消息直接确认，不改变任务。

初始预算建议 12 个步骤、3 次重规划、40k 总 Token、5 分钟。模型调用前在任务锁事务内预留输入估算加最大输出，并创建 RESERVED attempt。正常返回结算实际用量，供应商不返回 usage 或请求结束状态未知时用保守估算。恢复时把旧 epoch 未结算预留转换为 ESTIMATED，不能直接释放以免重复消耗。实际超出估计时如实记账并停止后续调用。预算不足时尝试仅靠已有持久证据确定性生成 PARTIAL；没有可交付证据则 FAILED，原因 budget_exhausted。

## API SSE 与前端

保留 M1 注册、登录、任务、报告和 SSE 路由，新增模型配置与取消。所有 API、SSE、报告引用按认证租户过滤。工具调用事件不包含密钥、完整原始网页或内部推理文本。

| 接口 | M2 行为 |
|---|---|
| PUT /api/v1/model-config | 建立新版本，密钥缺省沿用旧值，显式 replace 才更换，返回脱敏配置 |
| POST /api/v1/model-config/probe | 限流、短时探测，返回逐项 capabilities 与脱敏错误 |
| POST /api/v1/research/tasks | goal 继续必需，约束和预算可选且受服务器上限约束；幂等摘要包含有效请求与配置版本 |
| POST /api/v1/research/tasks/{id}/cancel | 重复取消幂等，已完成返回 409，其余接受后返回当前状态 |
| GET /api/v1/research/tasks/{id} | 附 plan、活动调用摘要、最新 event_seq、预算和 report_available，保证同一个一致性快照 |
| GET /api/v1/research/tasks/{id}/sources/{source_id} | 证据片段、标题、URL、时间及版本 |
| GET /api/v1/research/tasks/{id}/report | 原字段 markdown 保留，增 kind、revision、citations、limitations |

事件继续使用字符串 seq，schema_version=1 的新增类型为向后兼容扩展。plan_updated、step_started、tool_started、tool_finished、replan_started、usage_updated、report_ready、task_status 为持久事件。tool_started 包含稳定 tool_call_id、attempt_no、tool 和参数摘要，tool_finished 包含结果状态、证据 ID 和安全摘要。模型文字流的参数片段可在 Worker 内合并，M2 前端只消费已提交的摘要和报告。

首次打开先获取一致性快照，再从其 seq 建流。快照需含重建工具卡片的活动状态。客户端 reducer 按 seq 去重，以调用 ID 归并尝试，明确展示“重试中”“取消中”“资料不足”。后续事件在本地更新任务状态，report_ready 才请求报告。避免每条工具事件都调用任务详情。

SSE 从 MySQL 读取，Last-Event-ID 优先于 after，游标格式错误或超前返回 422。事件初始保留 30 天，任务记录保存 min_event_seq；游标过期时发送不分配持久 seq 的 reset 控制消息，客户端关闭连接、重取快照并重建流。终态事件读取完关闭，前端主动停止重连。原始 HTML 禁用，外链协议校验，报告默认不加载远程图片。Web 同源 Cookie 和写请求 Origin 校验继续沿用。

## Redis 与配置契约

沿用 rp_<env>_research 队列和 research:run:v1 载荷 tenant_id、task_id、generation。Asynq 管理自己的内部 Key。数据库是租约、取消、预算与事件事实来源。

应用限流 Key 为 rp:<env>:rl:v1:<operation>:<tenant_id>:<UTC-minute>，String 计数器，首次写设 120 秒 TTL，递增与设过期使用 Lua 原子执行。M2 初始 operation 为 task-create、model-probe 和 login；login 按规范账号摘要与可信来源地址分别限流。配额值通过部署配置设定。

M2 默认不引入工具缓存。后续确有需要再引入带工具版本和授权边界的缓存，避免 stale 资料及跨租户泄漏。生产 Queue Redis 独立容量及持久化，部署恢复后用 MySQL Outbox 对账重建派发。

## 验收与发布

| 验收组 | 场景及结果 |
|---|---|
| Provider | 无首包超时和 429 限次重试；401 直接失败；流中断不执行残缺工具调用；预算不被重试绕过 |
| Tool | 参数无效可修正，未知工具有限次纠正；SSRF IPv4/IPv6、DNS 重绑定、重定向和解压超限被拒 |
| Durable execution | 重复派发只领取一次；调用后提交前宕机可重试；旧 epoch 写入失效；报告只有一份已发布终态 |
| Cancel | 活 Worker 接受取消后停止新增调用；完成竞争按行锁裁决；失联 Worker 在租约回收后收口 |
| SSE | 断线、重复事件、快照与流竞争、过期游标均能恢复；工具调用及结果正确关联 |
| Migration | 从实际 M1 数据卷升级后，旧任务、报告和幂等重放可读；新用户及新研究流程正常 |
| Research quality | 10 个固定技术题，至少 8 个交付可用报告；所有引用 ID 合法，关键结论人工核查证据支撑 |
| Deployment | 本机当前 Compose/Podman 组合复测 API、Worker、MySQL 8.4、Redis 7.4；重启、队列丢失及迁移恢复可验证 |

新增针对恢复竞争与流片段的测试。模型与工具使用确定性夹具进行日常验证，真实模型评估单独运行并记供应商、模型、配置版本及执行日期。观测包含 task_id、trace_id、phase、attempt、延迟、队列等待、租约回收、引用覆盖及 Token，指标避免高基数租户标签。

发布前先验证 002 迁移和旧数据兼容，启动新版 Worker 后放行真实研究。现有 Podman 网桥放行规则是本机运行环境的临时修复，部署验收需验证重启后的容器互通。若服务回滚，M2 进行中的任务先停止领取并由兼容 Worker 排空，不能让旧 Worker 执行新 runtime_version 的任务。

## 待确认与实现决策

- 搜索供应商和首个真实模型待配置，代码用适配器并允许本地桩验收。联网调用的真实成本测试需要可用凭据。
- 本稿的预算、并发、保留时间和验收阈值为建议初值，集成测试后记录 ADR。
- M2.1 先完成持久化和恢复，再接真实模型；M2.2 与 M2.3 的契约冻结后才联调 Runtime。
- 本稿尚未实施迁移、修改应用或推送 GitHub，也尚未同步飞书。下一次实施以 M2.1 的迁移与故障验收为入口。
