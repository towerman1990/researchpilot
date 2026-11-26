# 公开知识库与 MCP

## 网页演示

在已配置个人模型或启用平台试用的工作台，勾选「先查公开知识库」，输入：

> HttpClient 如何在复用连接池的同时应对 DNS 记录变化？

Worker 在规划外部采集前检索内置的公开文档摘要。如果用户要求审批计划，检索在批准后执行。结果写入任务检查点并作为证据归档，报告引用保留摘要的资料库版本和官方出处。证据充足时直接生成报告。证据不足时进入现有采集流程，可按规划调用已启用的 Microsoft Learn MCP；该客户端从固定 HTTPS 端点发现工具，校验查询参数契约，只调用白名单中的只读搜索工具，并把预设主题映射成固定的公开查询词。

首版语料随程序打包，包含人工编写的摘要和指向官方资料的链接。运行记录显示知识库检索步骤，Worker 日志记录检索模式和命中数量。

## 开启向量混合检索

默认仅使用关键词检索。若要演示中文问题匹配英文资料，可向 Worker 进程提供以下三个配置。使用 Compose 时，可通过本地覆盖文件向 Worker 传递环境变量并挂载密钥文件。

| 环境变量 | 用途 |
| --- | --- |
| `KNOWLEDGE_EMBEDDING_URL` | OpenAI 兼容的完整 HTTPS embeddings 端点 |
| `KNOWLEDGE_EMBEDDING_MODEL` | 具备中英跨语言能力的嵌入模型名称 |
| `KNOWLEDGE_EMBEDDING_KEY_FILE` | 可供进程读取的 API Key 文件路径 |

宿主机进程也可使用本地 OpenAI 兼容嵌入服务。显式设置 `KNOWLEDGE_ALLOW_LOOPBACK_EMBEDDING=true`，端点使用 `http://127.0.0.1:<port>/v1/embeddings` 或 IPv6 回环字面地址。此模式的密钥文件可留空；容器内的回环地址只指向当前容器。远程服务仍使用公开 HTTPS 地址和密钥文件。

首次使用时，Go 精确检索会批量嵌入内置资料并缓存向量。查询时发送研究目标到配置的嵌入服务，使用余弦相似度与关键词排名融合。上传的附件和账户历史不会发给嵌入服务。不可用时使用关键词结果。

## 从外部 MCP 客户端检索

构建 `go build -o researchpilot-mcp ./cmd/researchpilot-mcp`，在支持 stdio 的客户端中添加该可执行文件的绝对路径。Windows 可将输出文件名改为 `researchpilot-mcp.exe`。可以为此进程单独配置上述嵌入变量。

- `search_knowledge` 接收 `query` 和可选的 `limit`，返回资料库版本、检索模式、片段 ID、摘要和官方链接。
- `get_evidence` 接收片段 `id`，返回同一公开数据集中的证据。试试 `httpclient-dns`。

该服务通过 [官方 Go SDK](https://github.com/modelcontextprotocol/go-sdk) 提供标准输入输出传输，独立于线上 API 和数据库。

## 复现检索评估

在仓库根目录运行 `go run ./cmd/researchpilot-knowledge-eval`。当前版本包含 11 道固定题，其中 9 道有预标注的相关片段，2 道在资料库中没有答案。JSON 输出包含每题的预期片段、检索顺序和耗时，以及各检索策略的 Recall@1/3/5、MRR@5、P95 和无答案题仍返回候选的次数。

默认运行关键词基线。配置上面的嵌入变量后，命令会在同一组题上对比关键词、纯向量和混合检索。向量检索出错会结束评估，避免把回退结果计入向量成绩。P95 包含查询阶段嵌入请求，不包含首次索引构建。当前语料仅 5 个片段，这些结果适合演示和回归检查，不能代表大规模语料上的检索质量。报告引用是否真正支持结论，仍用现有的引用复核流程单独评估。

## 可选 Milvus 后端

### 本地 Compose

先独立启动 Milvus：

```bash
docker compose -f compose.milvus.yaml up -d --wait
```

宿主机通过 `http://127.0.0.1:19530` 连接，可用 `MILVUS_LOCAL_PORT` 更改宿主机端口。容器内通过 `http://milvus:19530` 连接。嵌入式 etcd 和向量数据都写入命名卷 `milvus-data`，重建容器时卷会保留。宿主机只开放回环地址上的 Milvus API，健康检查和 etcd 端口仅在容器内部使用。该配置面向本地演示。

在宿主机上按下方步骤用评估命令准备集合后，设置 `KNOWLEDGE_EMBEDDING_URL`、`KNOWLEDGE_EMBEDDING_MODEL`、`KNOWLEDGE_EMBEDDING_KEY_FILE`（宿主机文件路径）和 `KNOWLEDGE_MILVUS_COLLECTION`，再启动工作台：

```bash
docker compose -f compose.yaml -f compose.milvus.yaml -f compose.milvus-worker.yaml up -d --build
```

Worker 等待 Milvus 健康检查通过后启动，嵌入密钥以文件方式挂载。启动工作台前也需要按 README 完成原有的模型主密钥初始化。

### 使用现有实例与准备集合

部署可访问的 Milvus 2.6 实例，并使用进程可访问的 HTTP(S) 地址。可以按 [Milvus Standalone 官方指南](https://milvus.io/docs/v2.6.x/install_standalone-docker.md)启动本地实例。HTTP 适用于本机或隔离网络，远程实例应使用 HTTPS 和认证。设置 `KNOWLEDGE_MILVUS_URL`（例如 `http://127.0.0.1:19530`），需要认证时设置 `KNOWLEDGE_MILVUS_TOKEN_FILE`。容器内运行 Worker 时，地址按容器网络配置。

1. 同时配置嵌入服务，运行 `go run ./cmd/researchpilot-knowledge-eval`。命令在 Milvus 默认数据库中创建或复用两套专用集合，分别建立 COSINE FLAT 和 HNSW 索引。集合名来自资料、模型和向量内容，命令会写入、刷盘、加载数据；使用允许创建集合和写数据的测试实例或账号。
2. 若要让工作台或本地 MCP 服务使用 Milvus，从 JSON 的 `milvus_comparison.searches` 选一个不含 `_synthetic_` 的 `collection`，为该进程额外设置 `KNOWLEDGE_MILVUS_COLLECTION`，并使用与评估一致的嵌入模型和配置。运行时只执行检索，关键词和 RRF 仍由 Go 计算。未配置集合时沿用 Go 精确检索，Milvus 查询失败时回退到关键词。
3. 用 `KNOWLEDGE_MILVUS_BENCH_ROWS` 增加规模（如 4096）。额外数据是固定随机种子的合成干扰向量，用于索引性能实验。上限为 20,000 行且总向量分量不超过 1,000 万。合成集合不能给工作台复用。

比较部分对同一题只做一次查询嵌入，三个后端用同一套 Float32 精度的向量。JSON 分别记录查询嵌入 P95、各后端搜索 P95（Milvus 含 REST 往返，不含建索引）、标注 Recall@5，以及与 Go 精确 Top 5 的重合率。HNSW 固定 `M=16`、`efConstruction=100`、`ef=64`。同时检查 `indexed_rows`，小集合可能由 Milvus 扫描未建索引的段。合成规模结果不能当作真实文档召回成绩。

## 工作台回归验收

工作台启用平台试用后，在仓库根目录运行 `pwsh -File scripts/smoke-knowledge.ps1`。也可传入 `-Credential (Get-Credential)` 使用已配置模型的账户。脚本提交一项知识库任务，验证任务成功、SSE 中的 `retrieval_mode` 和命中条数，以及报告引用。默认要求 `hybrid_milvus`，Go 混合检索和纯关键词环境可分别指定 `-ExpectedMode hybrid` 或 `-ExpectedMode keyword`。

## 2026-11-26 本地实测

使用 FastEmbed 0.9.1 运行 `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`，在 5 个真实资料片段和固定 11 道题上，关键词、纯向量、混合的 Recall@5 分别为 100%、94.4%、100%。三者 Recall@1 均为 83.3%，两道无答案题均未返回候选。这次小样本未测出混合相对关键词的召回提升。

使用相同模型加入合成干扰向量至 4,096 行，查询嵌入 P95 为 14.86 ms，Go、Milvus FLAT、Milvus HNSW 搜索 P95 分别为 3.80、67.04、50.28 ms；两个 Milvus 后端与 Go 的 Top 5 重合率均为 100%。这些数字来自单机单轮，Milvus 耗时含 REST 往返。

MySQL 研究任务集成测试接入同一真实本地嵌入服务和 Milvus，确认 Worker 记录 `hybrid_milvus` 命中事件，并把资料摘要写入报告引用。该用例使用测试模型响应完成报告生成，线上配置未变。

## 首版验收范围

测试覆盖公开样例的关键词排序、向量融合及失败降级、MCP 工具发现和调用，以及研究任务引用知识库证据。可持久化的私有文档库和异步导入管理仍待开发。更广泛的检索质量和索引性能结论需要扩充真实语料与题集。
