# M4 Windows 桌面原型：MyGo

## 当前结论

用户已确认 MyGo 指 egoist/mygo。已在 Windows amd64 上固定 MyGo v0.10.0，使用现有 ResearchPilot HTTPS Web 工作台作为桌面窗口内容。原型 Go 测试通过，`ResearchPilot.exe` 与 Windows NSIS 安装器均已成功构建。

本结论仍属候选方案，ADR 005 尚未冻结。当前原型验证了编译、打包与 URL 安全策略。真实部署登录、跨端历史、SSE 断线恢复、报告导出、安装卸载和 Tauri 同用例比较仍需验收。

## 设计边界

- Go 只负责窗口和导航策略，前端由部署中的 `web/` 应用提供。桌面端与浏览器共享同一服务端账号、任务和报告。
- WebView 使用自己的持久会话存储，浏览器首次登录互不共享。桌面端首次需要单独登录。
- 不注册 Go IPC，不绑定原生服务，不向网页授予 MyGo 的原生调用权限。
- 默认起始地址为 `https://researchpilot.towerman1990.cn/`。测试环境可设置 `RESEARCHPILOT_DESKTOP_URL`；仅允许 HTTPS，或用于本机开发的 localhost HTTP。地址不得携带用户信息、查询参数或片段。
- 站内导航要求 scheme 和 host 与起始地址完全相同。站外网页导航被取消，只有用户主动点击的 HTTP(S) 链接才交由系统浏览器打开。脚本弹窗和非 Web 协议均拒绝。
- 生产构建关闭 DevTools。安装器当前未签名，不能视为正式面向公众的发布物；Windows SmartScreen 和签名证书仍待产品发布决策。

## 本地构建

从 `apps/desktop` 执行：

~~~powershell
go test ./...
go run github.com/egoist/mygo/cmd/mygo@v0.10.0 build -platform windows/amd64
~~~

已验证输出：`build/windows-amd64/ResearchPilot.exe` 和 `build/windows-amd64/researchpilot-setup-0.1.0-amd64.exe`。安装器由 MyGo 使用 NSIS 构建。正式构建和安装检查应使用 MyGo CLI 固定版本，不使用 `@latest`。

开发后端可在本机 HTTP 模式运行：

~~~powershell
$env:RESEARCHPILOT_DESKTOP_URL = 'http://localhost:8080/'
~~~

关闭终端后此环境变量不保留。生产默认地址仍为 HTTPS 域名。不要把凭据放进桌面 URL 或命令参数。

## M4 桌面验收表

| 项目 | 状态 | 通过条件 |
| --- | --- | --- |
| Go 单元测试 | 已通过 | 拒绝公网 HTTP、URL 凭据、查询和片段；区分同源与域名相似的站外 URL |
| Windows amd64 EXE | 已构建 | MyGo v0.10.0 生成独立可运行文件 |
| NSIS 安装包 | 已构建 | MyGo v0.10.0 生成当前用户安装器 |
| 运行及 WebView2 | 待验 | Windows 启动、缺运行时提示、窗口尺寸及状态恢复 |
| 登录与跨端数据 | 待验 | 桌面独立登录后看到同一账号的任务、报告；浏览器端新建的任务可同步查看 |
| SSE 重连 | 待验 | 中断网络后自动恢复且事件不重复或丢失 |
| 外链和弹窗 | 待验 | 点击站外 HTTP(S) 链接打开系统浏览器；脚本弹窗、伪造相似域名及非 Web scheme 被拦截 |
| 报告导出 | 待验 | 报告在桌面端可读取、复制或下载，文件内容完整 |
| 安装卸载 | 待验 | 当前用户无管理员权限安装，卸载后不误删用户数据 |
| Tauri 对照 | 待验 | 对同一 Web 窗口、登录、SSE、外链、安装卸载和权限边界执行同组检查 |
| 代码签名 | 未配置 | 发布给普通用户前签名 EXE、安装器与卸载器并验证签名状态 |

## 桌面壳决策

MyGo v0.10.0 的 Windows 安装器已在当前 Windows amd64 工作站成功生成，符合团队现有 Go 技术栈，也不需要为了共享 Web UI 重写 React。Tauri 2 的 Rust/C++ 构建与权限能力已有文档比较，但本轮没有编译 Tauri 原型。因此目前不宣布 MyGo 全面对比胜出。ADR 005 待真实桌面验收完成后再决定。

官方参考：[MyGo 0.10.0](https://github.com/egoist/mygo/releases/tag/v0.10.0) · [快速开始](https://mygo.run/docs/getting-started) · [前端与信任边界](https://mygo.run/docs/frontend) · [Windows 分发与签名](https://mygo.run/docs/distribution) · [Tauri 权限](https://v2.tauri.app/security/capabilities/) · [Tauri Windows 安装器](https://v2.tauri.app/distribute/windows-installer/)
