# 项目概览

ResearchPilot 是技术研究工作台。用户提交研究目标，系统规划步骤，通过只读工具收集资料，并生成带来源引用的报告。Web 工作台提供账号、历史任务和 SSE 实时进度。M1 提供演示闭环，M2 已实现真实研究路径的基础，验收进度见 [当前进度](current-progress.md)。

| 路径 | 用途 |
| --- | --- |
| [cmd/researchpilot](../cmd/researchpilot) | Go API、Worker 和迁移入口及实现 |
| [web](../web) | React 工作台、事件归并及测试 |
| [migrations](../migrations) | M1 初始 SQL 与 M2 增量迁移 |
| [compose.yaml](../compose.yaml) | 本地 MySQL、Redis、迁移、API 和 Worker 服务 |
| [scripts](../scripts) | 本地初始化、验证、烟测和 Git 同步 |
| [docs](../docs/README.md) | 产品需求、技术设计、ADR 和验收记录 |

本地启动与开发依赖见 [README](../README.md)。更新已有数据库前参考 [M2 实施记录](../docs/09-M2-IMPLEMENTATION.md)。各设备自行配置本地凭据和数据库。需要解密既有模型配置时，须通过受控渠道保管对应的模型主密钥。

M3 已创建 MyGo v0.10.0 的 Windows amd64 原型和 NSIS 安装包，桌面壳仍属候选，ADR 005 待真实安装与同用例验收。详见 [桌面原型记录](../docs/13-M3-DESKTOP.md) 和 [路线图](../docs/07-ROADMAP.md)。
