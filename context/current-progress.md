# 当前进度

更新于 2026-11-18。本轮父提交为 5ee4a1d；本文件随 M2 收尾代码一同版本化。最新 CI 请核对当前提交对应的 Actions。

## 当前阶段

M1 已完成。M2 工程收尾、最终整组真实评估和助手关键证据复核完成：8/10，待用户体验终验。不能把该结论扩展为生产部署、桌面交付或独立人工审计已经完成。

## 本轮实现

- Provider、计划、工具注册与证据选择分别拆入 internal/model、internal/agent、internal/tools、internal/evidence；持久执行器仍在 cmd 组装。
- 完整响应 / 流式工具片段校验、持久模型重试上限、依赖图校验和拓扑执行、Observe 证据覆盖及有限重规划。
- 网页章节定位、去导航、按问题选择片段；公开 GitHub Wiki 可读取原始 Markdown。主来源优先保留额度，补充来源去重。
- PARTIAL 摘录报告、SSE 快照和过期游标 reset、终态事件留存、Redis 原子限流。
- 模型能力探测、密钥保留更新 / 撤销、版本化主密钥轮换。
- 004 至 006 增量迁移，模型错误码持久化。迁移校验兼容 LF / CRLF，继续拒绝 SQL 内容变化。
- 可选 HTTPS DNS 解决 Fake-IP，保持 TLS / 公网地址验证 / IP 固定；明确连接前失败零 Token 记账。DeepSeek JSON 模式关闭思考，输出额度 4096。
- 十题真实评估 CLI、浏览器烟测、SSE 长连接脚本，CI 增加 Redis 与浏览器测试。

## 验证

- Go 单元、vet、前端四项 reducer 测试及 Vite 7.3.8 构建通过。
- 独立 MySQL 测试库通过升级、恢复、取消、旧 epoch、PARTIAL、留存 reset、主密钥轮换 / 撤销、模型调用上限及证据不足不误报成功。
- Redis 并发限流和消息丢失后的补偿投递通过。
- Linux Go 1.25 容器已通过早期 race；最终证据选择和 Observe 版本通过 WSL 原生 Go 1.27.3 race / MySQL / Redis 测试。曾有重复容器测试卡在 apk 安装，已停止该临时容器，改为复用本机工具链。
- 完整独立 Compose 镜像、迁移、M1 烟测和 Windows Edge 无头浏览器通过。
- 100 条直连 SSE 各收到相同八条事件，无乱序 / 重复 / 缺项，最终 p95 500 ms（早期 471 ms）；未经过生产反向代理。
- 最终同一整组固定十题 8/10 完整报告，82,002 Token，所有成功引用合法；助手逐份复核关键事实，两个 MySQL 取证失败不计通过。此前诊断轮没有冒充最终验收，完整经过见 docs/11-M2-EVALUATION.md。
- GitHub 未认证 API 配额耗尽已复现；降级公开页面需从结构化状态确认提交 SHA，再读取该提交的公开 README，不转发 API 凭据。完整官方 API 模式仍可通过本地 GITHUB_READ_TOKEN 扩大配额。
- 云端最终验证结果以当前提交对应 GitHub Actions 为准，不能沿用历史基线。

## 已知限制

- 用户确认先使用指定官方 URL，不配置 Brave；真实搜索尚未验收。
- npm 官方审计仍有 picomatch 构建依赖链四项 high，最新发布 4.0.7 仍受影响；没有强制降级 Vite。最终应用镜像不含 Node/npm。
- 完整 OpenTelemetry、生产反代压测、持久层进一步拆包、Windows 安装包及腾讯云发布属于后续工作；当前不能声称已完成这些项目。
- 原数据卷未被验收环境覆盖，独立工作台运行在本机 8088。该环境的加密主密钥单独保存在已忽略目录，需与其数据库一起保管。
- 新决策和来源已补充 decisions，详细操作与验收见 docs/10-M2-CLOSEOUT.md。

## 下一步

- 用户查看八份研究报告并确认体验终验；真实 Brave 搜索仍可选，两个 MySQL 个案待官方资料可达后复测。
- M3 聚焦腾讯云 Web 上线、HTTPS、备份恢复和发布回滚；桌面壳比较与 Windows 交付移至 M4。
- 保留并备份验收环境的数据库与本地主密钥；两者不提交公共仓库。

## M3 启动

- 目标为腾讯云首尔地域 amd64，IPv4 43.166.14.25，公网域名 researchpilot.towerman1990.cn。已新增生产 Compose、Caddy HTTPS、MySQL 备份和手动发布镜像工作流。
- 独立本地环境通过 HTTPS 登录、任务、SSE 和报告，压缩备份在另一套新卷恢复后核对报告记录。当前先进行受限 HTTP 联调；新服务器 DNS、正式 HTTPS 及云端构建发布待验收。
- 已确认 MyGo 指 egoist/mygo，完成与 Tauri 2 的文档比较。ADR 005 等待同一组 Windows 原型验收。
- 详见 [M3 双端与发布](../docs/12-M3-RELEASE.md)。

- 协议由 RESEARCHPILOT_SCHEME 切换；示例先用 http，正式发布前改为 https，由 Caddy 自动申请证书。HTTP 联调使用临时账号，不提交真实模型密钥。

## 2026-11-18 范围调整

- 用户决定优先 Web 上线，桌面端整体移至 M4。已构建的 MyGo v0.10.0 Windows 原型和安装器保留，桌面 ADR 待 M4 验收。
- M3 仍需确认服务器登录方式、DNS、镜像发布、生产 HTTPS、真实研究及恢复演练。

## 平台模型试用开发

- 已实现每账号 10 个默认模型任务额度；事务内串行计数，幂等重放不重复扣除，个人密钥不消耗额度。平台密钥通过文件注入并在任务创建时加密快照。
- 本地独立 MySQL 验证 15 个并发任务与 10 次上限、重放、额度耗尽和个人密钥切换；迁移验证与 Web 构建通过。
- 服务器部署需要恢复 SSH、配置平台试用密钥；真实 DeepSeek 及线上验收待完成。单账号限制不能阻止重复注册。

## 2026-11-19 线上试用验证

- 服务器已部署 researchpilot-local:trial-v1，迁移 007 完成。迁移前数据库备份位于服务器受限目录 /opt/researchpilot/backups/before-trial-v1.sql.gz，原镜像和原 env 备份保留。
- HTTPS 独立验收账号初始显示 10 次，真实提交后剩余 9 次；使用服务器 DeepSeek 密钥的官方 Go 文档研究任务成功并取得报告。第 10/11 次边界、并发及个人密钥优先已在本地独立 MySQL 测试；未在生产批量消耗额度。
- 服务器构建使用 GOMAXPROCS=1、Go -p 1，以降低 2 GiB 内存机器的构建压力。试用密钥和模型主密钥都留在服务器受限目录，不提交仓库。

## macOS 与 Linux 文档

- 增加 macOS / Linux Docker Compose 使用指南和 POSIX shell 密钥初始化脚本。脚本保留已有主密钥；Docker 容器使用非 root UID，需要密钥文件可读且宿主机目录受限。
- macOS 与 Linux ARM64 仅提供架构说明，未在实际设备验证；生产镜像发布目前为 linux/amd64。
