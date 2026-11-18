# M2 收尾与放行记录

更新于 2026-11-18。状态：工程收尾与最终整组真实评估已完成（8/10），助手证据复核通过，待用户体验终验。云端 CI 以本次提交对应 Actions 为准。

## 本次交付

- 独立的 internal/model Provider，提供 Generate / Stream。流式工具按 index 合并，拒绝 ID 变更、缺失结束标记、断流、不完整 JSON、异常 finish_reason 和超限响应。半截工具参数不会交给 Runtime。
- 独立的 internal/agent 计划契约和 internal/tools 注册表。计划带稳定步骤 ID，校验重复 ID、缺失依赖和依赖环；按拓扑顺序串行执行，前置失败阻断依赖步骤。拒绝未知字段、未知工具、无效参数和超限计划。计划格式错误最多修正一次；总计最多三个规划轮次，每轮最多四个串行工具步骤。
- 模型每次请求前预留预算，尝试身份与逻辑步骤绑定。最多三次尝试的上限跨 Worker 恢复保持有效，429 和临时故障按 Retry-After 加抖动等待，401 / 403 不做任务级重试。
- 工具证据、步骤检查点、尝试结果和完成事件在同一任务锁事务提交。运行日志记录 task_id、trace_id（当前与 task_id 相同）、步骤、attempt 和耗时，不记录提示词、正文或凭据。
- 预算不足、deadline 或最终失败时，如已有证据，确定性生成 PARTIAL 报告。正文明确标为摘录及未完成，不把来源片段冒充模型研究结论。取消保持 CANCELLED，不发布部分报告。
- 一致性任务快照包含事件 seq、重放下界、预算和最近调用尝试。前端先获取快照，再从其 seq 订阅。过期游标收到无持久 seq 的 reset 后重新获取快照；终态停止重连。
- Worker 每小时清理超出留存期的终态任务事件，默认 30 天。删除事件和推进 min_event_seq 原子提交；报告、来源和检查点不随事件删除。
- Redis Lua 原子窗口限流，覆盖登录来源 / 账号、注册来源、任务创建和模型探测。Redis 不可用返回 503，超额返回 429 和 Retry-After；不信任客户端 X-Forwarded-For。
- 配置保存时空 API Key 保留最新有效密钥，显式填写则创建替换版本。DELETE 配置撤销全部已保存版本；新任务拒绝已撤销配置，既有研究在后续调用和续租时检查撤销。
- 探测基础对话、JSON Object、原生 JSON Schema、工具调用和流式工具调用。探测只验证响应，绝不执行供应商返回的工具。
- AES-GCM 加密支持主密钥版本及事务式轮换；所有旧模型配置版本可重加密，任务绑定的模型版本保持不变。
- 补齐按问题选择证据、同 URL 去重、主来源优先额度与 Observe 语义覆盖；覆盖 JSON 可修正一次，无法支撑问题时仍返回 PARTIAL。
- GitHub API 限流可降级到公开页面中的已验证提交和该 SHA 的公开 README，元数据缺失会标注，不使用浮动分支或转发凭据。
- 新增十题官方资料评估入口 evaluate，以及无额外 npm 依赖的真实浏览器烟测。CI 增加 Redis 和浏览器验收。

## 数据库与兼容性

新增迁移 004_m2_closeout.sql、005_model_attempt_identity.sql 与 006_model_failure_reason.sql，不修改 001 至 003 的 SQL。现有 M1 / M2 数据需先备份，再执行 migrate；应用启动要求第六项迁移成功且无 dirty 标记。

新增字段包括 model_configs.revoked / key_version、research_tasks.min_event_seq，以及 model_attempts.logical_key / attempt_no / error_code。逻辑模型尝试有租户、任务、步骤和尝试号唯一约束。已有记录保持可读。

迁移校验兼容 LF / CRLF 检出，并通过 .gitattributes 固定 SQL 与 Shell 换行；真实 SQL 内容变化仍被拒绝。新任务 runtime_version 为 m2.2。执行器继续兼容旧任务、报告、检查点和既有幂等记录。

## 验收矩阵

| 项目 | 结果与边界 |
| --- | --- |
| Go 单元及静态检查 | 本地通过；含 Provider 流片段、错误分类、参数契约和限流拒绝策略 |
| MySQL 迁移与恢复 | 独立临时库通过 M1 数据升级、租约、取消、检查点恢复、旧 epoch 拦截和模型重试上限 |
| PARTIAL / 留存 | 数据库测试通过；过期终态事件删除后发送 reset，报告来源仍保留 |
| Redis | 原子并发限流和队列消息删除后的新 generation 补偿派发通过；测试只删除随机专用队列 |
| 模型配置 | 确定性夹具通过五项能力探测、空值保留、主密钥轮换及撤销 |
| 出站安全 | 私网 IPv4 / IPv6、混合 DNS、重绑定、重定向、解压后超限测试通过 |
| 前端 | 四项 reducer 测试、生产构建通过 |
| 完整栈与浏览器 | 独立 Compose 栈已完成一次镜像构建、迁移、M1 烟测和真实无头浏览器验收；最终版本复测结果见当前进度 |
| 云端 CI | 本次提交对应 [Actions](https://github.com/towerman1990/researchpilot/actions/workflows/ci.yml) 为最终结果来源；不以历史基线替代当前代码检查 |
| 真实模型质量 | 最终同一整组 8/10；八份均完成助手关键证据复核，两份 MySQL 取证失败不计通过。见 [真实评估](11-M2-EVALUATION.md) |
| Brave 搜索 | 用户选择先使用指定官方 URL 验收，当前不配置 Brave。搜索适配器维持可选，真实搜索尚未验收 |

## 复现与操作

本地现有验证命令：

~~~powershell
pwsh -File ./scripts/verify.ps1
~~~

浏览器烟测需要 Node.js 22.12+ 和 Chrome / Edge，默认使用 Windows Edge 或 Linux google-chrome。可通过 BROWSER_PATH 指定浏览器，通过 TEST_BASE_URL 指定工作台。测试创建独立账号，模型配置只使用测试字符串并最终撤销，不调用真实模型。

~~~powershell
$env:TEST_BASE_URL='http://localhost:8088'
node scripts/browser-smoke.mjs
~~~

真实十题评估必须先通过工作台保存可用模型配置，并保持 API / Worker / MySQL / Redis 运行。下列命令在 WSL 的标准 Compose 项目下运行，会产生真实供应商费用。若有多个有效租户，通过 EVAL_TENANT_ID 明确选择；不要把真实 tenant_id 或评估产物写入公共仓库。

~~~bash
mkdir -p .cache
sudo docker compose run --rm --no-deps   -e EVAL_ALLOW_LIVE=1 api evaluate > .cache/m2-evaluation.json
~~~

使用独立验收项目时，需同时传入该项目实际使用的 -p 和 -f 参数。evaluate 通过数据库 Outbox 创建十个有界任务，交由真实 Redis / Worker 执行；不会直连执行器绕过派发。输出包含模型版本、时间、状态、Token、报告与引用证据。自动成功数不能代替人工质量分数，quality_accepted 默认始终为 false。

人工放行要求至少 8 / 10 份可用报告，逐项核对关键断言、来源支撑、引用合法性和局限说明，并保存审查记录。PARTIAL 摘录不计为完整可用报告。

## 密钥轮换

MODEL_MASTER_KEY_VERSION 默认为 1，MODEL_MASTER_KEY_FILE 指向当前主密钥文件。旧版本通过 MODEL_MASTER_KEY_FILE_V1、MODEL_MASTER_KEY_FILE_V2 等指向受限文件。不得将文件值放进命令行、日志或仓库。

1. 先备份数据库和旧密钥，准备新密钥文件。
2. 暂停新任务和配置写入，并让旧 Worker 排空；为所有进程准备旧新版本文件。
3. 将目标版本与文件配置到维护进程，执行 researchpilot rotate-model-keys。
4. 成功后升级 API / Worker 至相同配置，验证旧配置可读和新任务正常，再恢复流量。
5. 在备份恢复演练及旧实例下线完成前保留旧密钥；失败时整个重加密事务回滚，不逐行留下混合结果。

## 已知限制与后续边界

- 日常研究仍使用完整 JSON 响应；原生工具流已具备契约及探测，但不直接驱动工具执行。
- 计划已支持有界依赖图与串行拓扑执行，Observe 持久记录步骤证据覆盖和重规划原因。复杂并行调度及语义覆盖评分尚未实现。
- 当前已将 Provider、计划与工具契约拆成独立包，持久 Runtime 仍由 cmd 组装；完整 internal/task、agent、events 等拆分未完成。
- 基础结构化日志已落地，尚无完整 OpenTelemetry 链路、指标仪表盘及生产反向代理压力验收。本地 100 条直连 SSE 均收到相同的 8 条事件，最终 p95 延迟 500 ms，无缺项、重复和乱序。
- Vite 已升级至 7.3.8，修复其自身已知问题。官方 npm 审计仍报告 picomatch <=4.0.7 的 ReDoS 漏洞及其依赖传播，共四项 high；核验时最新发布版仍为 4.0.7。没有强制降级 Vite 或掩盖审计结果。它位于构建依赖，最终 Alpine 应用镜像不含 Node/npm；上线前仍应跟进上游修复。
- 十题真实评估和助手证据复核已完成；真实搜索、用户体验终验与生产部署仍分别记录。云端 CI 以对应提交的 Actions 为准，不能宣称这些后续工作都已完成。


## 本机 Fake-IP 故障与修复

真实调用曾因 api.deepseek.com 被系统 DNS 解析到 198.18.2.138 而在出站策略中被拒绝。这是保留地址段，不作为可连接公网 IP。没有放宽 SSRF 拦截。

可在本地 .env 配置 OUTBOUND_DOH_URL=https://1.1.1.1/dns-query，再更新 Compose 服务。默认留空仍使用系统 DNS。自定义 HTTPS DNS 必须由管理员指定公网 IP 字面量地址，验证 TLS 证书，不跟随重定向；当前可选模式解析 IPv4 A 记录，并对每个结果执行公网检查、固定实际连接 IP。它不会修改 Windows、WSL 或代理软件的全局 DNS。遇到解析服务超时仍会限次重试，不保证第三方 DNS 的可用性。

已增加明确的 DNS 策略拒绝、解析失败、TLS 错误、认证错误和输出截断提示；模型尝试持久保存脱敏错误码。确定未发出请求的 DNS / 策略失败记零 Token，未知结果仍保守记账；过去的失败记录不会因此自动重写。

DeepSeek 官方接口的结构化 JSON 请求显式设置 thinking.type=disabled，避免默认思考占满输出额度；其他供应商不发送该扩展。模型最大输出额度为 4096，预算预留使用同一额度。截断输出不执行，但仍保留已知实际 usage，不能把已知值丢成估算。参考：[DeepSeek 思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)。

配置撤销仅禁用本应用保存的版本，不代替供应商控制台撤销 API Key。轮换主密钥则只改变密文保护方式，不改变供应商 API Key。

最终十题结果及逐份复核见 [M2 真实评估](11-M2-EVALUATION.md)。


## 最终本地复测

最终代码通过 WSL 原生 Go 1.27.3 的 race、MySQL 与 Redis 集成测试；另有早期 Go 1.25 容器 race 记录，最终 Go 1.25 检查以 GitHub CI 为准。完整镜像启动、迁移、M1 烟测、Windows Edge 浏览器操作、四项前端测试和 Vite 构建均通过。最终 100 条直连 SSE 各收到八条相同事件，p95 为 500 ms（早期为 471 ms），不等同于生产反代压测。

浏览器覆盖注册登录、研究完成、报告展示、刷新恢复、密钥留空保留和撤销。主密钥轮换另在临时测试库验证；单连接池也可保留旧密钥，避免更新事务自锁。真实报告与诊断产物留在本机忽略目录，旧数据卷未被覆盖。
