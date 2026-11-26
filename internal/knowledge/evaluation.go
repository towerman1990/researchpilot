package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

const EvaluationVersion = "demo-queries-v1"

type evaluationCase struct {
	id, query string
	relevant  []string
}

var evaluationCases = []evaluationCase{
	{"dns-zh", "HttpClient 怎样更新 DNS 解析缓存？", []string{"httpclient-dns"}},
	{"dns-en", "How does PooledConnectionLifetime help a long-lived client pick up DNS changes?", []string{"httpclient-dns", "httpclient-factory"}},
	{"pool-zh", "每次请求都创建和释放 HttpClient 为什么会导致端口耗尽？", []string{"httpclient-pooling"}},
	{"pool-en", "Does a new HttpClient instance share an existing connection pool?", []string{"httpclient-pooling", "httpclient-factory"}},
	{"factory-zh", "IHttpClientFactory 如何通过复用处理器来管理连接？", []string{"httpclient-factory", "httpclient-pooling"}},
	{"routing-zh", "ASP.NET Core 怎样给同一组最小 API 路由统一加授权？", []string{"aspnet-routing"}},
	{"routing-en", "Can MapGroup apply shared metadata to a group of endpoints?", []string{"aspnet-routing"}},
	{"cancel-zh", "Go 父 context 被取消后，子 context 的 Done 通道会怎样？", []string{"go-context"}},
	{"cancel-en", "What does calling the cancel function returned by WithCancel do?", []string{"go-context"}},
	{"uncovered-crypto", "How does RSA-OAEP padding work?", nil},
	{"uncovered-index", "为什么 MySQL 联合索引遵守最左前缀规则？", nil},
}

type Evaluation struct {
	Corpus     string            `json:"corpus"`
	Queries    string            `json:"query_set"`
	Model      string            `json:"embedding_model,omitempty"`
	Strategies []StrategyResult  `json:"strategies"`
	Milvus     *MilvusComparison `json:"milvus_comparison,omitempty"`
}

type StrategyResult struct {
	Mode                 string        `json:"mode"`
	RecallAt1            float64       `json:"recall_at_1"`
	RecallAt3            float64       `json:"recall_at_3"`
	RecallAt5            float64       `json:"recall_at_5"`
	MRRAt5               float64       `json:"mrr_at_5"`
	UncoveredWithResults int           `json:"uncovered_with_results"`
	P95Milliseconds      float64       `json:"p95_ms"`
	Cases                []QueryResult `json:"cases"`
}

type QueryResult struct {
	ID           string   `json:"id"`
	Query        string   `json:"query"`
	Relevant     []string `json:"relevant_ids"`
	Results      []string `json:"result_ids"`
	Milliseconds float64  `json:"ms"`
}

func EvaluateDemo(ctx context.Context) (Evaluation, error) {
	var embed Embed
	var err error
	if endpoint := os.Getenv("KNOWLEDGE_EMBEDDING_URL"); endpoint != "" {
		embed, err = configuredEmbed(endpoint)
		if err != nil {
			return Evaluation{}, fmt.Errorf("configure embeddings: %w", err)
		}
	}
	compare := os.Getenv("KNOWLEDGE_MILVUS_URL") != ""
	if compare && embed == nil {
		return Evaluation{}, errors.New("milvus comparison requires an embedding provider")
	}
	index, err := Demo(ctx, embed)
	if err != nil {
		return Evaluation{}, fmt.Errorf("build demo index: %w", err)
	}
	report, err := evaluateIndex(ctx, index)
	if err != nil {
		return Evaluation{}, err
	}
	if embed != nil {
		report.Model = strings.TrimSpace(os.Getenv("KNOWLEDGE_EMBEDDING_MODEL"))
	}
	if compare {
		report.Milvus, err = compareMilvus(ctx, index, report.Model)
	}
	return report, err
}

func evaluateDemo(ctx context.Context, embed Embed) (Evaluation, error) {
	index, err := Demo(ctx, embed)
	if err != nil {
		return Evaluation{}, fmt.Errorf("build demo index: %w", err)
	}
	return evaluateIndex(ctx, index)
}

func evaluateIndex(ctx context.Context, index *Index) (Evaluation, error) {
	report := Evaluation{Corpus: DemoVersion, Queries: EvaluationVersion}
	modes := []searchMode{keywordMode}
	if index.embed != nil {
		modes = append(modes, vectorMode, hybridMode)
	}
	for _, test := range evaluationCases {
		for _, id := range test.relevant {
			if _, ok := index.Get(id); !ok {
				return Evaluation{}, fmt.Errorf("unknown relevant passage %q in %s", id, test.id)
			}
		}
	}
	for _, mode := range modes {
		result, err := evaluateStrategy(ctx, index, mode)
		if err != nil {
			return Evaluation{}, err
		}
		report.Strategies = append(report.Strategies, result)
	}
	return report, nil
}

func evaluateStrategy(ctx context.Context, index *Index, mode searchMode) (StrategyResult, error) {
	result := StrategyResult{Mode: string(mode)}
	latencies := make([]float64, 0, len(evaluationCases))
	covered := 0
	for _, test := range evaluationCases {
		start := time.Now()
		passages, _, err := index.search(ctx, test.query, 5, mode, false)
		elapsed := elapsedMS(start)
		if err != nil {
			return StrategyResult{}, fmt.Errorf("%s/%s: %w", mode, test.id, err)
		}
		query := QueryResult{ID: test.id, Query: test.query, Relevant: append([]string{}, test.relevant...), Results: make([]string, 0, len(passages)), Milliseconds: elapsed}
		for _, passage := range passages {
			query.Results = append(query.Results, passage.ID)
		}
		latencies = append(latencies, elapsed)
		result.Cases = append(result.Cases, query)
		if len(test.relevant) == 0 {
			if len(query.Results) > 0 {
				result.UncoveredWithResults++
			}
			continue
		}
		covered++
		result.RecallAt1 += recall(query, 1)
		result.RecallAt3 += recall(query, 3)
		result.RecallAt5 += recall(query, 5)
		for n, id := range query.Results {
			if slices.Contains(query.Relevant, id) {
				result.MRRAt5 += 1 / float64(n+1)
				break
			}
		}
	}
	result.RecallAt1 /= float64(covered)
	result.RecallAt3 /= float64(covered)
	result.RecallAt5 /= float64(covered)
	result.MRRAt5 /= float64(covered)
	result.P95Milliseconds = p95(latencies)
	return result, nil
}

func recall(query QueryResult, k int) float64 {
	hits := 0
	for _, id := range query.Results[:min(k, len(query.Results))] {
		if slices.Contains(query.Relevant, id) {
			hits++
		}
	}
	return float64(hits) / float64(len(query.Relevant))
}
