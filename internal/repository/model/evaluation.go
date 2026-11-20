package model

type EvaluationSummary struct {
	FinishedTasks uint64 `json:"finished_tasks"`
	Succeeded     uint64 `json:"succeeded"`
	Partial       uint64 `json:"partial"`
	Failed        uint64 `json:"failed"`
	Cancelled     uint64 `json:"cancelled"`
	Reports       uint64 `json:"reports"`
	CitedReports  uint64 `json:"cited_reports"`
	Helpful       uint64 `json:"helpful"`
	NotHelpful    uint64 `json:"not_helpful"`
}

type EvaluationReviewItem struct {
	TaskID     string  `json:"task_id"`
	Goal       string  `json:"goal"`
	Status     string  `json:"status"`
	FinishedAt string  `json:"finished_at"`
	Rating     *string `json:"rating,omitempty"`
}

type ResearchEvaluation struct {
	Summary     EvaluationSummary      `json:"summary"`
	ReviewQueue []EvaluationReviewItem `json:"review_queue"`
}
