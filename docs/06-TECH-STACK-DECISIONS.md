# 技术选型与 ADR

## HTTP 框架比较

2026-11-19 用户明确指定 Hertz，工作区已切换 Hertz 服务器。第三轮重构已迁移全部 API、静态资源和 SSE 到 Hertz 原生路由，本地集成回归覆盖任务写入、认证和事件重放。

| 维度 | Gin | Hertz |
|---|---|---|
| 集成 | 以 net/http 为基础，便于复用常见测试及中间件。 | CloudWeGo RequestContext，官方有 SSE 与适配器；审查鉴权、OTel 和代理交互。 |
| 迁移成本 | 原设计基线，开发路径短。 | Handler、绑定、错误与中间件适配；Go 应用服务与 Runtime 保持相同接口。 |
| 验证 | 验证反代 Flush、重连和优雅退出。 | 用相同套件测取消传播、慢客户端、长连接内存、断流重连与优雅退出。 |

建议以 Gin 为对照，优先验证 Hertz 是否适合日常开发。用同机器、同代理、同队列和数据库，固定 100 条 SSE 长连并运行短请求。若契约全过，p95、资源开销和维护成本可接受，可选 Hertz，框架吞吐宣传值不进入验收。[Gin 官方文档](https://gin-gonic.com/) · [Hertz SSE](https://www.cloudwego.io/docs/hertz/tutorials/basic-feature/sse/)

## 桌面壳比较

用户已确认 MyGo 指 egoist/mygo。本轮以官方发布的 0.10.0 与 Tauri 2 作对比，ADR 005 在两个 Windows 原型验收后确定。[MyGo 发布记录](https://github.com/egoist/mygo/releases/tag/v0.10.0)。

| 维度 | Tauri 2 | egoist/mygo |
|---|---|---|
| React 复用 | 系统 WebView，Rust 壳，使用同一 React 构建产物。 | 系统 WebView 模式可用同一构建产物，Go 原生 UI 模式需要另写 UI。 |
| 原生权限 | Capabilities 按窗口等范围声明命令权限，已有跨平台分发文档。 | Go 壳、类型化 TS IPC；应验证远程来源隔离、命令白名单与导航限制。 |
| 发布与维护 | Windows WebView2、macOS WKWebView，团队需维护 Rust 构建和签名。 | 同平台 WebView 前提，上游提供分发与更新文档；0.x 应固定版本并验证升级、签名和回滚。 |

两者都能承载现有网页应用。Windows 首次交付的成本主要取决于安装器、签名、WebView2 缺失时的体验，以及远程页面与原生接口的隔离。

| 验证项 | Tauri 2 | egoist/mygo 0.10.0 |
| --- | --- | --- |
| 构建与安装 | Rust、Windows C++ Build Tools，可产出 NSIS 或 MSI，文档列出多种 WebView2 安装策略 | Go 壳，Web 前端模板使用 Bun，可产出当前用户安装的 NSIS，需实测缺少 WebView2 的机器 |
| 权限 | 按窗口、命令和来源配置 capabilities，首版远程工作台不授予原生 IPC | 提供类型化 Go/TS IPC，首版远程工作台不注册绑定，并验证导航到外链时的隔离 |
| 发布维护 | 安装和签名链路较清晰，团队增加 Rust 工具链 | Go 壳符合现有技术栈，需要实测 NSIS、签名、卸载保留数据和更新回退 |

建议各做一个 Windows x64 原型。若 MyGo 在相同用例中验证安装、登录及独立会话、跨端历史、SSE 断线恢复、外链、报告导出与卸载，可优先考虑 MyGo，以减少长期维护的语言栈。签名、安装和权限隔离的实测结果决定最终选型。首版桌面壳使用同一个 HTTPS 工作台，Web 与桌面分别登录同一账号，并共享服务端数据。

[Tauri 权限](https://v2.tauri.app/security/capabilities/) · [Tauri Windows 安装器](https://v2.tauri.app/distribute/windows-installer/) · [MyGo 架构](https://mygo.run/docs/architecture) · [MyGo 分发](https://mygo.run/docs/distribution)

## 决策记录

| ADR | 状态及决策 | 后果或复评触发 |
|---|---|---|
| 001 | 更新：单 Agent，采用 Eino 编排 | 现有数据库检查点、租约和预算机制保留；每轮计划、工具执行和观察已拆为 Eino 节点，仍需验证恢复与取消语义。 |
| 002 | 已确认：通用 OpenAI-compatible 接口 | 能力矩阵和适配器契约测试成为必需。 |
| 003 | 继承：MySQL 持久状态、Asynq 调度、SSE 推送；outbox 与 fencing 机制为本稿提案 | 需故障注入验证；未来可替换队列。 |
| 004 | 用户决定：Hertz | Hertz 原生路由已完成本地 SSE、认证和任务取消回归。 |
| 005 | 已确认双端；共享 React 为基线；Tauri / MyGo 待定 | 桌面权限和分发验收通过后更新 ADR。 |
| 006 | 待评审：P0 自动执行，P1 审批 | 首版减少人工阻塞；计划版本为后续审批预留。 |

## 明确待决项

- 工程验证后关闭 ADR 004、005。明确 Windows 发布方式。本稿将 macOS 构建验证放在 M0，正式分发暂列 P1。
- 产品评审 P0 是否需要一次点击确认计划，以及正式预算、留存与证据覆盖阈值。
- 部署方选定搜索供应商、默认模型与允许域，确认签名与密钥管理方式。
