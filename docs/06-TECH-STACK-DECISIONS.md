# 技术选型与 ADR

## HTTP 框架比较

| 维度 | Gin | Hertz |
|---|---|---|
| 集成 | 以 net/http 为基础，便于复用常见测试及中间件。 | CloudWeGo RequestContext，官方有 SSE 与适配器；审查鉴权、OTel 和代理交互。 |
| 迁移成本 | 原设计基线，开发路径短。 | Handler、绑定、错误与中间件适配；Go 应用服务与 Runtime 保持相同接口。 |
| 验证 | 验证反代 Flush、重连和优雅退出。 | 用相同套件测取消传播、慢客户端、长连接内存、断流重连与优雅退出。 |

建议以 Gin 为对照，优先验证 Hertz 是否适合日常开发。用同机器、同代理、同队列和数据库，固定 100 条 SSE 长连并运行短请求。若契约全过，p95、资源开销和维护成本可接受，可选 Hertz，框架吞吐宣传值不进入验收。[Gin 官方文档](https://gin-gonic.com/) · [Hertz SSE](https://www.cloudwego.io/docs/hertz/tutorials/basic-feature/sse/)

## 桌面壳比较

原对话只出现 my-go / MyGo，缺少作者、链接或版本。官方仓库和文档确认 **egoist/mygo** 是可复用 React 的 Go 桌面框架，发布页查阅时标记 v0.9.2。下表仅针对这个具名候选；用户的具体指代仍需本人确认。

| 维度 | Tauri 2 | egoist/mygo |
|---|---|---|
| React 复用 | 系统 WebView，Rust 壳，使用同一 React 构建产物。 | 系统 WebView 模式可用同一构建产物，Go 原生 UI 模式需要另写 UI。 |
| 原生权限 | Capabilities 按窗口等范围声明命令权限，已有跨平台分发文档。 | Go 壳、类型化 TS IPC；应验证远程来源隔离、命令白名单与导航限制。 |
| 发布与维护 | Windows WebView2、macOS WKWebView，团队需维护 Rust 构建和签名。 | 同平台 WebView 前提，上游提供分发与更新文档；0.x 应固定版本并验证升级、签名和回滚。 |

建议先用 Tauri 2 保持交付基线。若 MyGo 指代确认，用相同 React 产物验证 Windows 安装与清洁卸载、macOS 构建、登录回调、SSE 恢复、凭证安全、签名及更新回滚。Go 壳的维护收益与这些结果共同决定是否切换。[Tauri Capabilities](https://v2.tauri.app/security/capabilities/) · [MyGo 仓库](https://github.com/egoist/mygo) · [架构](https://mygo.run/docs/architecture) · [发布记录](https://github.com/egoist/mygo/releases)

## 决策记录

| ADR | 状态及决策 | 后果或复评触发 |
|---|---|---|
| 001 | 继承：单 Agent、Go 自研 Runtime | 显式控制预算和检查点；P2 再评估并行协作。 |
| 002 | 已确认：通用 OpenAI-compatible 接口 | 能力矩阵和适配器契约测试成为必需。 |
| 003 | 继承：MySQL 持久状态、Asynq 调度、SSE 推送；outbox 与 fencing 机制为本稿提案 | 需故障注入验证；未来可替换队列。 |
| 004 | 待定：Gin / Hertz | 双框架适配验证通过后记录选型。 |
| 005 | 已确认双端；共享 React 为基线；Tauri / MyGo 待定 | 桌面权限和分发验收通过后更新 ADR。 |
| 006 | 待评审：P0 自动执行，P1 审批 | 首版减少人工阻塞；计划版本为后续审批预留。 |

## 明确待决项

- 用户确认 MyGo 是否指 egoist/mygo。若是另一项目，提供官方仓库链接。
- 工程验证后关闭 ADR 004、005。明确 Windows 发布方式。本稿将 macOS 构建验证放在 M0，正式分发暂列 P1。
- 产品评审 P0 是否需要一次点击确认计划，以及正式预算、留存与证据覆盖阈值。
- 部署方选定搜索供应商、默认模型与允许域，确认签名与密钥管理方式。
