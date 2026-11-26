package execution

import (
	"context"
	"encoding/json"
	"fmt"

	"researchpilot/internal/knowledge"
)

func (e execution) retrievePrivateKnowledge(ctx context.Context, goal, library string) ([]evidence, error) {
	value, err := e.checkpoint(ctx, privateKnowledgeStep, func(ctx context.Context) (string, error) {
		chunks, err := e.store.KnowledgeChunks(ctx, e.Tenant, library)
		if err != nil {
			return "", err
		}
		passages := make([]knowledge.Passage, 0, len(chunks))
		for _, chunk := range chunks {
			id := fmt.Sprintf("%s:%d", chunk.VersionID, chunk.Ordinal)
			passages = append(passages, knowledge.Passage{
				ID: id, Title: fmt.Sprintf("%s · 第 %d 版 · 片段 %d", chunk.Name, chunk.Revision, chunk.Ordinal),
				URL: "knowledge:" + id, Excerpt: chunk.Excerpt,
			})
		}
		hits, err := knowledge.SearchPrivate(ctx, passages, goal, 4)
		if err != nil {
			return "", err
		}
		observation := knowledgeObservation{Mode: "private_keyword", Sources: make([]evidence, 0, len(hits))}
		for _, hit := range hits {
			observation.Sources = append(observation.Sources, evidence{
				Title: hit.Title, URL: hit.URL, Excerpt: hit.Excerpt, Type: "knowledge",
			})
		}
		raw, err := json.Marshal(observation)
		return string(raw), err
	})
	if err != nil {
		return nil, err
	}
	return decodeKnowledgeSources(value), nil
}

func taskSourceURL(task, source string) string {
	return "/api/v1/research/tasks/" + task + "/sources/" + source
}
