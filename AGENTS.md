# ResearchPilot 协作指南

## 开始工作

1. 运行 `git status --short --branch`，确认当前分支及本地改动。保留已有改动。
2. 阅读 [项目概览](context/project-overview.md)、[架构](context/architecture.md)、[当前进度](context/current-progress.md) 和 [决策记录](context/decisions.md)。根据任务查阅 [README](README.md)、[技术文档索引](docs/README.md)、[M2 设计](docs/08-M2-DESIGN.md)、[M2 实施记录](docs/09-M2-IMPLEMENTATION.md) 和相关代码。
3. 需要接续其他设备的工作时，从当前分支运行 `pwsh -File ./scripts/sync-before-work.ps1`。脚本在工作区有改动时停止。拉取成功后重新读取上下文。若上游未配置或发生冲突，检查 Git 状态并保留现场，按用户指示处理。
4. 以当前代码、迁移和可复现测试核对已实现行为。使用技术文档理解目标和契约，未核实的事项标记为 TODO，并说明待确认内容。

## 结束工作

1. 更新 [当前进度](context/current-progress.md)，记录本次改动、验证结果、阻塞项及可接续的下一步。
2. 检查 [决策记录](context/decisions.md)，补充新增或变更的决策、原因和来源。无决策变更时在进度中注明。项目范围或架构变更时同步更新对应摘要。
3. 检查 Git diff，确保上下文只收录可公开的信息。环境变量值、API Key、Token、密码、私钥、真实连接串和转储文件留在设备本地。提交前也要检查已跟踪文件和暂存区内容。
4. 运行 `pwsh -File ./scripts/sync-after-work.ps1` 查看状态。提交和推送须经用户授权。另一台设备拉取已推送的 Git 提交后即可接续上下文。
