# macOS 和 Linux 部署指南

本指南使用仓库根目录的 `compose.yaml`，适合个人电脑上的本地使用和功能体验。应用只监听 `127.0.0.1:8080`，数据库和 Redis 没有映射到宿主机端口。面向公网的安装、TLS、备份和恢复请参阅 [M3 发布文档](12-M3-RELEASE.md)，不要直接把本地配置暴露到公网。

## 安装 Docker

- **macOS**：按芯片类型安装并启动 [Docker Desktop for Mac](https://docs.docker.com/desktop/setup/install/mac-install/)。Docker Desktop 包含 Docker Engine、CLI 和 Compose。Apple Silicon 与 Intel 下载对应版本。
- **Linux**：按发行版安装并启动 Docker Engine 和 CLI，再安装 [Docker Compose 插件](https://docs.docker.com/compose/install/linux/)。也可使用 Docker Desktop for Linux。某些系统需要使用 `sudo docker ...`，具体取决于 Docker 的权限配置；不要混用不同 Docker 用户或 daemon 的数据卷。

在终端确认客户端、服务和 Compose 均可用：

```sh
docker version
docker compose version
docker info
```

Linux 如果只有 `sudo docker` 可用，以下 Docker 命令均加 `sudo`，但初始化脚本仍由项目文件所有者执行。通过 Docker 管理用户组会赋予较高的系统权限，按 Docker 官方文档评估后配置。

项目目录需要由 Docker Desktop 共享；如果遇到文件挂载权限问题，请检查 Docker Desktop 的文件共享设置。Linux 启用了 SELinux 或 rootless Docker 时，文件映射权限取决于系统策略，遇到权限错误应先检查 daemon 日志和文件标签，不要关闭系统安全策略。

## 获取项目并启动

安装 Git 和 OpenSSL，在终端执行：

```sh
git clone https://github.com/towerman1990/researchpilot.git
cd researchpilot
sh scripts/init-local.sh
docker compose up --build -d
docker compose ps -a
curl -fsS http://localhost:8080/healthz
```

健康检查应返回 `{"ok":true}`。然后访问 [http://localhost:8080](http://localhost:8080)，注册并登录。首次构建需要下载基础镜像、Go 模块和 npm 依赖，速度取决于网络；MySQL 初始化和数据库迁移也需要时间。`migrate` 容器执行成功后退出是正常现象。

初始化脚本生成 `.cache/model-master.key`，重复运行会保留已有文件。此文件用于加密用户模型 API Key，**请与数据库备份分别安全保存**；丢失后已有模型密钥无法解密。脚本将 `.cache` 设置为当前用户可访问，首次生成的密钥文件设为容器可读，因为应用镜像使用 UID 65532。Docker 管理员仍可访问挂载的文件。现有密钥不会被脚本修改权限，若迁移已有环境请自行核对容器可读性。

macOS Apple Silicon 和 Linux ARM64 可先按本机架构尝试构建。项目当前生产发布工作流只构建 `linux/amd64`，本指南没有宣称 ARM64 已完成验收；如基础镜像或构建失败，请记录芯片架构和完整错误，优先确认镜像是否支持相应架构。模拟 amd64 的性能和兼容性需要单独验证。

## 模型和可选工具

在网页「模型设置」填写 OpenAI 兼容服务的 Base URL、模型名称和 API Key。默认允许的模型域名见 `MODEL_ALLOWED_HOSTS` 的应用默认值；本地默认允许 DeepSeek 和 OpenAI。未配置个人模型、且本地未启用平台试用时，任务运行固定的 M1 演示，不会进行真实联网研究。

可选地将 `.env.example` 复制为 `.env`，再按需编辑 DNS、模型代理、Brave Search 和 GitHub 令牌：

```sh
cp -n .env.example .env
# 用自己熟悉的编辑器填写 .env，勿提交真实凭据。
docker compose up -d
```

修改环境变量后需重新运行 `docker compose up -d` 以重建对应容器。密钥只在本地配置，勿提交 Git。平台提供的十次试用需要部署方自行配置服务器密钥和 `deploy/compose.trial.yaml`；本地默认 Compose 不启用该功能。

## 日常管理与排查

```sh
docker compose ps -a
docker compose logs --tail=100 api worker migrate
docker compose stop
docker compose start
docker compose down
```

`down` 默认保留数据库和 Redis 卷；`down --volumes` 会删除这些数据，请只在明确需要清空本地数据时使用。升级前先备份数据库和对应模型主密钥。现有云端备份与恢复示例见 [M3 发布文档](12-M3-RELEASE.md)，操作前应针对自己的 Compose 文件及环境变量调整。

常见问题：

- `docker: command not found`：安装 Docker CLI 和 Compose 插件，或启动 Docker Desktop。
- `Cannot connect to the Docker daemon`：确认 Docker Desktop 或 Docker Engine 已启动，以及当前用户有访问权限。
- 8080 端口已占用：释放本机端口，或同时调整 Compose 的端口映射和 `PUBLIC_ORIGIN`；浏览器来源与 `PUBLIC_ORIGIN` 必须一致。
- API 或 Worker 无法读取模型主密钥：检查 `.cache` 的宿主机文件共享、密钥文件可读权限和系统安全策略。保留原密钥，不要重新生成覆盖。
- 镜像或依赖下载超时：检查目标机器到 Docker Hub、Go 模块和 npm 仓库的网络连通性。
- 登录后出现演示报告：在模型设置中保存可用的模型配置；未配置时演示模式属于预期行为。

现有 `scripts/smoke.ps1` 是 PowerShell 7 烟测脚本。macOS 和 Linux 用户可选安装 PowerShell 7 后运行 `pwsh -File ./scripts/smoke.ps1`；它会创建测试账号和任务。
