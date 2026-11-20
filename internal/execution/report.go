package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"researchpilot/internal/common/textutil"
	"researchpilot/internal/outbound"
	"researchpilot/internal/repository"
)

type reportDraft struct {
	Summary  string `json:"summary"`
	Findings []struct {
		Text    string   `json:"text"`
		Sources []string `json:"sources"`
	} `json:"findings"`
	Limitations []string `json:"limitations"`
}

func renderReport(draft reportDraft, sources []evidence) (string, []evidence, error) {
	byID := make(map[string]evidence)
	for _, s := range sources {
		byID[s.ID] = s
	}
	if len(draft.Findings) == 0 || len(draft.Findings) > 8 || len(draft.Limitations) > 16 {
		return "", nil, errors.New("report_has_no_findings")
	}
	used := make([]evidence, 0)
	ordinals := map[string]int{}
	var body strings.Builder
	body.WriteString("# 研究报告\n\n")
	// Use plain, escaped text in Markdown to avoid unvalidated model URLs and HTML.
	escape := func(s string) string {
		for _, pair := range [][2]string{{"&", "&amp;"}, {"<", "&lt;"}, {">", "&gt;"}, {"[", "\\["}, {"]", "\\]"}, {"!", "\\!"}, {"(", "\\("}, {")", "\\)"}} {
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
		return s
	}
	for _, finding := range draft.Findings {
		if len(finding.Sources) == 0 || len(finding.Sources) > 12 || strings.TrimSpace(finding.Text) == "" || len(finding.Text) > 6000 {
			return "", nil, errors.New("finding_missing_citation")
		}
		body.WriteString(escape(finding.Text))
		for _, id := range finding.Sources {
			source, ok := byID[id]
			if !ok {
				return "", nil, errors.New("invalid_source_id")
			}
			ordinal := ordinals[id]
			if ordinal == 0 {
				used = append(used, source)
				ordinal = len(used)
				ordinals[id] = ordinal
			}
			body.WriteString(fmt.Sprintf(" [%d]", ordinal))
		}
		body.WriteString("\n\n")
	}
	body.WriteString("## 局限\n\n")
	for _, limitation := range draft.Limitations {
		body.WriteString("- " + escape(limitation) + "\n")
	}
	body.WriteString("\n## 参考资料\n\n")
	for i, source := range used {
		if _, err := outbound.ValidateURL(source.URL); err != nil {
			return "", nil, errors.New("invalid_source_url")
		}
		safeURL := strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(source.URL)
		body.WriteString(fmt.Sprintf("%d. [%s](<%s>)\n", i+1, escape(source.Title), safeURL))
	}
	return body.String(), used, nil
}

// A partial report quotes archived source excerpts. It makes no new model
// claims and incurs no additional tokens when the task budget is exhausted.
func publishPartial(ctx context.Context, tx *repository.ExecutionTx, d delivery, t taskRow, reason string) (bool, error) {
	rows, err := tx.PartialReportSources(ctx, d.Tenant, d.Task)
	if err != nil {
		return false, err
	}
	sources := make([]evidence, 0, len(rows))
	for _, source := range rows {
		if source.Excerpt != "" {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return false, nil
	}
	draft := reportDraft{Limitations: []string{"研究未完成，以下仅为已归档资料摘录，尚未形成完整研究结论。", fmt.Sprintf("结束原因：%s。", reason)}}
	for _, s := range sources {
		draft.Findings = append(draft.Findings, struct {
			Text    string   `json:"text"`
			Sources []string `json:"sources"`
		}{Text: "资料摘录：" + textutil.Clip(s.Excerpt, 1000), Sources: []string{s.ID}})
	}
	body, citations, err := renderReport(draft, sources)
	if err != nil {
		return false, err
	}
	body = strings.Replace(body, "# 研究报告", "# 部分研究成果", 1)
	if err := tx.SaveReport(ctx, d.Tenant, d.Task, body, "partial", citations); err != nil {
		return false, err
	}
	if err := tx.SetPartial(ctx, d.Tenant, d.Task); err != nil {
		return false, err
	}
	return true, eventUpdate(ctx, tx, d, t, "report_ready", map[string]string{"status": "PARTIAL", "error_code": reason})
}

func finishReport(ctx context.Context, tx *repository.ExecutionTx, d delivery, t taskRow, body, kind string, citations []evidence) error {
	if err := tx.SaveReport(ctx, d.Tenant, d.Task, body, kind, citations); err != nil {
		return err
	}
	if err := tx.SetTaskFinished(ctx, d.Tenant, d.Task, "SUCCEEDED", ""); err != nil {
		return err
	}
	return eventUpdate(ctx, tx, d, t, "report_ready", map[string]string{"status": "SUCCEEDED"})
}
