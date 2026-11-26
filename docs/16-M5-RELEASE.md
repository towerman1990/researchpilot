# M5 研究工作台增强

2026-11-26 已部署至 [ResearchPilot 生产工作台](https://researchpilot.towerman1990.cn)，对应提交 `552479a`。

| 功能 | 优先级 | 交付范围 |
| --- | --- | --- |
| GitHub 仓库深度分析 | P1 | 目录、源码和依赖清单固定到同一提交。支持 Go 和 JavaScript 清单，以及 npm 的锁定版本、部分传递依赖和工作区链接。 |
| 长期研究记忆 | P1 | 登录用户可以保存研究背景。新任务创建时保存快照，生成报告时可参考。访客工作台提供登录提示。 |
| 多 Agent 分工 | P2 | 新建研究可启用分工。证据分析员向撰稿员交接笔记，引用审核员在发布前复核。运行记录显示角色和检查点。 |

发布前完成数据库备份及完整性检查。上线后 API、就绪和队列健康检查通过，访客每日额度确认为 5 次。生产多 Agent 冒烟任务完成证据分析、撰写和引用复核，生成包含 3 条归档引用的报告。 [CI](https://github.com/towerman1990/researchpilot/actions/runs/45767935949) · [镜像构建](https://github.com/towerman1990/researchpilot/actions/runs/45768302267)
