# M4 Windows 桌面验收与发布

## 首版选型

选定 egoist/mygo v0.10.0 作为 Windows amd64 首版桌面壳。MyGo 与 Tauri 2.12.7 都通过了 Windows 安装、独立登录、跨端历史、SSE 断线重连和 Markdown 下载。两者的原型均拦截脚本弹窗。MyGo 能将用户主动点击的站外 HTTP(S) 链接交给系统浏览器，当前 Tauri 原型会拦截这类链接。MyGo 已通过同一组用例，并沿用项目现有的 Go 工具链。

## 客户端边界

- 应用默认打开 `https://researchpilot.towerman1990.cn/`。桌面和浏览器分别登录同一账号，任务历史和报告由服务端共享。
- 窗口内仅允许同源导航。用户主动点击的站外 HTTP(S) 链接由系统浏览器打开。脚本弹窗和其他协议由应用拦截。
- 桌面壳关闭生产 DevTools，应用配置不向页面授予原生调用权限。
- 本地联调可使用 `RESEARCHPILOT_DESKTOP_URL=http://localhost:8080/`。自定义地址须为 HTTPS，或本机回环 HTTP。地址不得携带凭据、查询或片段。

## Windows 验收记录

两套客户端均连接同一个本地真实 ResearchPilot 后端，使用独立 WebView2 配置和验收账号。浏览器侧先创建任务，桌面端登录后读取既有报告。桌面端再创建任务，验收时断开实际 SSE 连接并观察重新订阅，恢复 Worker 后检查终态。下载文件逐字节比对同一任务的服务端 Markdown。

| 场景 | MyGo v0.10.0 | Tauri 2.12.7 |
| --- | --- | --- |
| Windows x64 构建、WebView2 启动 | 通过 | 通过，Rust 1.90.0 MSVC |
| 独立登录和跨端历史 | 通过 | 通过 |
| 实际断开 SSE 后重连并看到完整报告 | 通过 | 通过 |
| 下载 Markdown，内容与 API 一致 | 通过 | 通过 |
| 脚本弹窗拦截 | 通过 | 通过 |
| 用户主动点击外链后打开系统浏览器 | 通过 | 当前原型拦截 |
| 当前用户 NSIS 安装和卸载 | 通过 | 通过，已安装程序跑完同场景 |

## 构建、安装和升级

在 `apps/desktop` 中运行：

~~~powershell
go test ./...
go run github.com/egoist/mygo/cmd/mygo@v0.10.0 build -platform windows/amd64
~~~

产物是 `build/windows-amd64/ResearchPilot.exe` 和 `build/windows-amd64/researchpilot-setup-0.1.2-amd64.exe`。运行安装器即可为当前 Windows 用户安装。Windows 上需要可用的 WebView2 Runtime。首次启动后用已有 ResearchPilot 账号登录。

首版采用手动升级。关闭已运行的客户端，核对新版安装器的版本和 SHA-256，然后运行新版安装器。升级构建沿用 `mygo.json` 的应用标识 `cn.towerman1990.researchpilot` 并递增版本。用户凭据和研究数据由服务器管理。覆盖升级已通过独立安装标识下的 `0.1.0 → 0.1.1` 验收。升级沿用原安装目录，替换了程序并保持 WebView2 的本地状态和持久 Cookie。卸载测试副本后，WebView2 测试配置目录仍存在，已有 ResearchPilot 安装的版本、路径和程序哈希均一致。

Windows x64 验收包 `0.1.2` 将 `apps/desktop/assets/researchpilot-icon.png` 打入应用、安装器和卸载器。安装时可在完成页创建桌面快捷方式，Windows 从已安装程序读取图标。验收包未签名，安装器 SHA-256 为 `ed43d6fe59ae058afbf521f62b8b49d6f85838ae11a11df6727d4cb6087d751b`。

## 正式签名与发布前验证

本机已通过 MyGo 的签名流程验收。临时证书写入了安装器、应用和卸载器三处 Authenticode 签名，证书及私钥随后删除。正式构建需使用可访问私钥的可信代码签名证书，并由 HTTPS RFC 3161 端点加盖时间戳。[Microsoft SignTool 指南](https://learn.microsoft.com/windows/win32/seccrypto/signtool)。

### 申请发布者签名

首次公开发行需要一个经验证的发布者身份。以个人身份申请时，先向代码签名服务商确认居住国家或地区、可用证件、签名显示名称和最终费用。SSL.com 提供个人验证（IV）代码签名，支持 USB 硬件令牌与 eSigner。USB 令牌和可接入 Windows 证书存储的云签名客户端可沿用下方 SignTool 流程。[SSL.com 代码签名产品](https://www.ssl.com/products/software-integrity/code-signing/) · [eSigner Windows Key Adapter](https://www.ssl.com/how-to/how-to-install-ssl-com-esigner-cloud-key-adapter-cka/)。

Microsoft Azure Artifact Signing 的个人公开信任验证限美国和加拿大。SignPath Foundation 的免费开源签名要求项目已经发布过所申请的产物形式。[Azure 资格说明](https://learn.microsoft.com/azure/artifact-signing/quickstart) · [SignPath 条件](https://signpath.org/terms)。

签名服务商审核通过后，在 Windows 发布机安装令牌驱动或云签名客户端，并按供应商说明登录或插入令牌。当前用户可用的证书可用以下命令确认，证书指纹和供应商的 HTTPS RFC 3161 时间戳地址用于下方构建。证件、账号认证信息及私钥由发布者在签名服务中管理。

~~~powershell
Get-ChildItem Cert:/CurrentUser/My -CodeSigningCert |
    Select-Object Subject, Thumbprint, NotAfter, HasPrivateKey
~~~


发布者证书已由 Windows 识别后，在仓库根目录执行以下命令。证书指纹可配置到发布工作站，私钥始终由证书存储或硬件提供。

~~~powershell
$ErrorActionPreference = 'Stop'
$env:RESEARCHPILOT_SIGNING_THUMBPRINT = '<证书指纹>'
$env:RESEARCHPILOT_SIGNING_TIMESTAMP_URL = '<HTTPS RFC 3161 地址>'
# 证书在本机存储时设为 LocalMachine，默认 CurrentUser。
$env:RESEARCHPILOT_SIGNING_STORE = 'CurrentUser'

$signer = (Resolve-Path 'apps/desktop/sign-windows.ps1').Path
$stage = New-Item -ItemType Directory '.cache/desktop-signed-0.1.2'
Copy-Item -LiteralPath @('apps/desktop/main.go','apps/desktop/go.mod','apps/desktop/go.sum') -Destination $stage.FullName
Copy-Item -LiteralPath 'apps/desktop/assets' -Destination $stage.FullName -Recurse
$config = Get-Content 'apps/desktop/mygo.json' -Raw | ConvertFrom-Json -AsHashtable
if (!$config.windows) { $config.windows = @{} }
$config.windows.signCommand = ('pwsh.exe -NoProfile -File "' + $signer + '" %1')
$config | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $stage.FullName 'mygo.json') -Encoding utf8
go run github.com/egoist/mygo/cmd/mygo@v0.10.0 build -platform windows/amd64 $stage.FullName
if ($LASTEXITCODE -ne 0) { throw 'Windows signed build failed.' }
~~~

MyGo 会为三处可执行文件调用同一个签名脚本。构建后在干净的 Windows 环境中检查安装器的发布者提示，安装后分别核对应用和卸载器的签名与时间戳，并记录安装包 SHA-256。签名有效与 SmartScreen 的信誉判断需分别验证。[Microsoft Windows 签名说明](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options)。
