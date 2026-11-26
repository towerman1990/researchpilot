# M3 Web 上线与发布

M3 以已通过 CI 的 M2 提交 `7168d00` 为起点。首发目标是在腾讯云运行 Web 服务。Windows 桌面客户端与桌面壳选型移至 M4。首发环境已启用 HTTPS；HTTP 仅用于受限联调。

| 交付 | 放行条件 |
| --- | --- |
| 腾讯云试部署 | 以固定镜像 digest 启动，先验证 HTTP 健康检查及临时账号的登录、任务、报告与 SSE；HTTPS 单独验收 |
| 数据恢复 | 在独立环境恢复备份，核对报告、模型配置解密和待执行任务补偿派发 |
| M4 桌面客户端 | MyGo 与 Tauri 比较及 Windows 安装包验收移至 M4，不作为 M3 上线条件 |
| 正式发布 | 完成服务器、域名和 HTTPS 验收，记录部署的 digest、备份位置、回退验证与负责人 |

M4 的比较对象已确认包含 egoist/mygo，已有原型见 [桌面原型](13-M3-DESKTOP.md)。文档对比与 ADR 状态见 [技术选型](06-TECH-STACK-DECISIONS.md)。比较采用相同的 React 工作台，重点验证 WebView2、构建工具链、安装卸载、导航与原生权限边界、代码签名和更新回退。首个原型应复用当前同源 Cookie 和 SSE 接口。桌面壳不保存模型密钥。独立安装包的实现取决于选型结果。

## 准备镜像

GitHub Actions 的 **Release image** 手动工作流只允许从 `main` 运行，先检查同一提交的 CI 成功，再把 Linux amd64 镜像发布到 GHCR。使用运行摘要给出的 `仓库@sha256:…` 作为部署输入。首发服务器为腾讯云首尔地域 amd64，IPv4 为 43.166.14.25。若 CVM 拉取 GHCR 不稳定，可把同一镜像同步到腾讯云 TCR，并使用目标仓库核实后的 digest。首次使用私有镜像时，在服务器以拉取权限登录镜像仓库。

## 腾讯云部署

1. 将 `deploy/` 放到服务器固定目录。首发域名为 researchpilot.towerman1990.cn，目标 IPv4 为 43.166.14.25（首尔）。核对域名 A 记录、安全组及现有 80/443 端口占用。若已有反向代理，复用现有入口，避免抢占端口。
2. 复制 `deploy/.env.example` 为 `deploy/.env`，保护文件权限。填写域名和镜像 digest，当前设置 RESEARCHPILOT_SCHEME=http。应用 PUBLIC_ORIGIN 与 Caddy 入口同步使用 HTTP。分别用 `openssl rand -hex 24` 生成应用及 MySQL root 密码，采用十六进制以避免 Go MySQL DSN 分隔符。已有数据库恢复时，保留对应的模型加密主密钥。全新环境用 `openssl rand -hex 32` 生成，文件需可由容器 UID 65532 读取，宿主目录限制访问。Compose 文件中的密钥字段填绝对路径。
3. 从仓库目录执行命令。MySQL 和 Redis 仅通过内部容器网络访问。Caddy 负责入口和 SSE 转发，https 模式下自动管理 TLS，MySQL 迁移成功后才启动 API 和 Worker。

~~~sh
docker compose --env-file deploy/.env -f deploy/compose.yaml pull
docker compose --env-file deploy/.env -f deploy/compose.yaml up -d
docker compose --env-file deploy/.env -f deploy/compose.yaml ps -a
curl -fsS "http://researchpilot.towerman1990.cn/healthz"
~~~

验收时用独立账号检查注册登录、任务、断线重连和引用报告。登录来源限流使用可信 Caddy 的转发地址。API 将 TCP 对端与 Compose 的 proxy 主机解析结果比较，仅接受匹配代理传来的单个合法 IP；解析失败或请求头异常时使用 TCP 对端地址。Caddy 必须覆盖客户端传入的 X-Forwarded-For，不得信任未经验证的上游转发头。

## 临时 HTTP 与 HTTPS 切换

当前 HTTP 仅用于受限联调，建议安全组将 80 端口限制为自己的公网 IP。HTTP 不加密浏览器与服务器之间的密码、会话和模型密钥；临时阶段使用独立测试账号，不填写真实模型密钥。可先验证页面、登录、演示任务及 SSE。

域名指向 43.166.14.25，Caddy 能访问证书服务且对外提供 80/443 后，将 deploy/.env 的 RESEARCHPILOT_SCHEME 改为 https，重新执行 compose up -d。API/Worker 的 PUBLIC_ORIGIN 与 Caddy 入口同步更新，登录 Cookie 自动启用 Secure 属性。客户端切换到 HTTPS 并重新登录，复测健康检查、登录、SSE 和报告。

无需提前手动申请证书，Caddy 可自动申请及续期，但证书签发成功仍须现场核实。[Caddy 自动 HTTPS](https://caddyserver.com/docs/automatic-https)。HTTP 联调不代表正式发布通过。模板未设置协议时默认 https，示例 env 显式选择临时 http。

## 升级、备份和回退

1. 暂停写流量并等待在途任务完成。查询运行中及等待中的任务后停止 API、Worker。记录旧镜像 digest，并用 `bash deploy/backup.sh /受限目录/时间戳.sql.gz` 备份 MySQL。将匹配的模型主密钥独立安全保存。备份期间不要执行 DDL。
2. 更新镜像 digest，执行 pull、up 和健康检查。确认 migrate 容器退出码为 0，再运行 HTTPS 烟测。若迁移状态为 dirty，检查已生效的 SQL 再决定如何恢复。
3. 应用回退前核对旧镜像与现有数据库结构的兼容性。如果兼容，恢复旧 digest，再执行 up 和烟测。
4. 若数据库需要回退，用独立项目和新数据卷恢复备份，保留当前卷供排查。在新栈只启动 MySQL 后，用下列命令导入，再用匹配的旧镜像和主密钥验证。切换前核对自备份以来的用户写入。Redis 队列可由 MySQL 的 Outbox 和任务回收机制补偿，仍需在演练中检查任务状态。

~~~sh
set -o pipefail
gzip -dc /受限目录/时间戳.sql.gz |
  docker compose -p researchpilot-restore --env-file deploy/.env -f deploy/compose.yaml exec -T mysql sh -ec '
    export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
    exec mysql -uroot
  '
~~~

## 本地验证与云端现状

2026-11-18 在独立 Podman Compose 栈使用 Caddy 本地 CA，通过 HTTPS 健康检查、注册登录、任务提交、SSE 完整回放和报告读取。将 MySQL 压缩备份导入另一套新数据卷，核对了同一任务的报告记录。模型配置解密和队列恢复需要另行做完整演练。

当前腾讯云首尔 amd64 服务器已部署并启用 HTTPS；宝塔 Nginx 占用 80，代理容器监听 443。本地构建镜像 `researchpilot-local:trial-v1`，独立验收账号已完成真实研究。线上以 HTTPS 验证 SSE 事件序列完整并通过 Last-Event-ID 回放；真实网络断开重连仍需单独检查。模型配置解密、队列恢复和正式镜像回退仍待完整演练。

## 访客与账号试用

访客可直接打开工作台，每天可提交 5 次任务，按北京时间零点重置。服务端对访客 Cookie 和可信代理识别的来源 IP 分别计数，同一来源 IP 下的多个浏览器共享上限。第 6 次提交返回 `429 guest_trial_exhausted`，工作台弹出登录和注册窗口。访客浏览器中的研究记录保留在访客会话内，登录后显示账户自己的记录。

上线前执行迁移 `019_guest_trial.sql`，确认 API 可通过配置的可信代理识别真实客户端 IP。启用平台试用模型后，访客可运行真实模型研究。未启用时使用演示任务。

### 账号试用

启用 deploy/compose.trial.yaml 后，未配置个人模型的已登录用户可创建 10 次使用平台模型的研究任务。次数按成功创建的任务扣除；同一个幂等请求、任务执行重试和恢复不重复计数，任务失败或取消不自动返还。配置自己的密钥后，新任务使用个人模型，不消耗试用额度。用户撤销个人密钥后，如仍有额度，可继续试用。注册账号本身不验证邮箱，单账号额度不能防止重复注册，需要结合注册治理和供应商预算控制。

服务器将 DeepSeek 试用密钥单独保存在受限文件，并在 deploy/.env 中设置 TRIAL_MODEL_API_KEY_PATH；可设置 TRIAL_MODEL_BASE_URL 和 TRIAL_MODEL_NAME。不要将密钥写入 env、日志或仓库。启用时在现有 Compose 命令后加入 -f deploy/compose.trial.yaml。API 读取密钥文件并将每个试用任务所用的模型密钥加密快照，Worker 仅需要既有模型主密钥。密钥轮换仅影响新任务；恢复备份时需保留原模型主密钥。

启用前确认 MySQL 备份、密钥可供容器 UID 65532 读取和模型出站域名白名单，检查迁移 007 执行情况。使用独立账号验收 10 次额度、第 11 次提示、个人密钥优先和真实模型调用。首次启用需服务器实际提供有效的试用密钥。

2026-11-19 首次试用部署：服务器已执行迁移 007 并启用试用服务。独立验收账号从 10 次降至 9 次，一次基于 Go 官方文档的真实研究成功并生成报告。当前服务器使用本地构建的 trial-v1 镜像；代码已提交并通过 CI，发布镜像工作流和固定 digest 部署仍待执行。
## 当前服务器的 Compose 命令

当前服务器必须同时使用三个 Compose 文件，才能保留宝塔 Nginx 的端口接入和平台试用密钥：

~~~sh
cd /opt/researchpilot/source
docker compose --env-file deploy/.env -f deploy/compose.yaml -f deploy/compose.nginx.yaml -f deploy/compose.trial.yaml ps -a
docker compose --env-file deploy/.env -f deploy/compose.yaml -f deploy/compose.nginx.yaml -f deploy/compose.trial.yaml up -d
~~~

`deploy/compose.nginx.yaml` 为现有 Nginx 提供本机 HTTP 转发端口，并由 Caddy 直接监听 443。修改任何入口前先核对线上实际配置与端口占用。`backup.sh` 只调用 MySQL，使用基础 Compose 文件，不改变其他容器。

## 2026-11-19 恢复演练

在隔离的源 MySQL 中创建测试报告、加密模型配置和已派发待恢复任务，压缩备份后导入另一个新 MySQL。以对应测试主密钥成功解密模型配置，确认报告内容；使用全新空 Redis，通过任务恢复与 Outbox 转发确认新的派发代次进入队列。随后以独立 Worker 从目标 Redis 消费恢复任务，完成演示任务并生成报告。该演练未导入生产数据，也未调用真实模型；生产恢复切换仍需按具体窗口演练。

## 2026-11-19 固定镜像发布与回退演练

- GitHub Actions 发布了提交 `bcbcad04adc2e42dcad67c7e64cbe1555d953a6f` 对应镜像。该次发布使用 `ghcr.io/towerman1990/researchpilot@sha256:c7e24aae56fa45b28e7031b750fc6fcf7dac4755e3973604d1decc722a719d96`；随后已更新为下方的可信代理版本。
- 发布前备份：服务器 `/opt/researchpilot/backups/before-ghcr-bcbcad0.sql.gz`；主密钥需单独保管。部署后独立账号通过 HTTPS 注册、登录、默认试用、真实研究、SSE 和报告检查。
- 短暂切换回原 `researchpilot-local:trial-v1`，等待启动后 HTTPS 健康检查通过；随后自动切回 GHCR 固定镜像，确认 API、Worker、MySQL、Redis 正常。该回退演练仅检查服务健康，未在旧镜像重新执行完整研究，也未执行数据库回退。
- 可信代理来源识别已部署；线上验证两个伪造转发地址被计入同一个真实来源额度。已模拟关闭 SSE 连接后携带 Last-Event-ID 重新连接并收到完整报告；浏览器自动重连和生产数据库恢复切换尚未演练。

## 2026-11-19 可信代理版本发布

- 提交 `743b4ff866919bbba01ed0f85f9d1594e65c6972` 的 CI 三项通过，镜像发布成功；线上使用 `ghcr.io/towerman1990/researchpilot@sha256:d11e3c65d44e6170f11ce0e83a93921d47b6e1446b93bb518b916dc077bb1d0f`。
- 切换前备份服务器数据库 `/opt/researchpilot/backups/before-proxy-743b4ff.sql.gz`，旧镜像与配置保存在 `/opt/researchpilot/releases/before-proxy-743b4ff/`。
- 线上两次伪造转发 IP 的登录请求计入同一个来源额度；隔离代理测试验证请求头被覆盖。新版本独立账号通过 HTTPS 登录、试用额度、真实研究、SSE 和报告检查。
- 账号试用次数不阻止重复注册；浏览器自动 SSE 重连、生产数据恢复切换及旧镜像完整研究回退仍需在受控窗口演练。隔离恢复演练已验证报告、加密模型配置、任务重新入队及 Worker 完成演示任务。

## 就绪与队列检查

- `/healthz` 检查 API 能否访问 MySQL。`/readyz` 还检查 Redis，适合发布时确认服务就绪。
- `/queuez` 在当前派发代次存在等待至少 120 秒的排队任务时返回 503。空队列只能说明当前没有积压，发布仍须通过任务完成验收。
- CI 的生产 Compose 冒烟测试通过 Caddy 提交演示任务并读取 SSE 与报告，随后启用试用覆盖配置，验证密钥在 API 进程可读以及试用额度可查询。
