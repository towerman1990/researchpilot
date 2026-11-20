package model

import "encoding/json"

type TaskSnapshot struct {
	ID             string         `json:"task_id"`
	Goal           string         `json:"goal"`
	ParentTaskID   *string        `json:"parent_task_id,omitempty"`
	CreationLogID  *string        `json:"creation_logid,omitempty" gorm:"column:creation_logid"`
	ReviewPlan     *string        `json:"review_plan,omitempty"`
	Status         string         `json:"status"`
	Phase          *string        `json:"phase"`
	Seq            uint64         `json:"event_seq,string" gorm:"column:event_seq"`
	Error          *string        `json:"error_code,omitempty" gorm:"column:error_code"`
	TokensUsed     uint32         `json:"tokens_used"`
	TokensReserved uint32         `json:"tokens_reserved"`
	TokenLimit     uint32         `json:"token_limit"`
	MinSeq         uint64         `json:"min_event_seq,string" gorm:"column:min_event_seq"`
	Calls          []CallSnapshot `json:"calls,omitempty" gorm:"-"`
}

type CallSnapshot struct {
	ID      string `json:"tool_call_id" gorm:"column:step_key"`
	Attempt uint32 `json:"attempt_no" gorm:"column:attempt_no"`
	Status  string `json:"status"`
}

type UsageDay struct {
	Day             string `json:"day"`
	ActualTokens    uint64 `json:"actual_tokens"`
	EstimatedTokens uint64 `json:"estimated_tokens"`
	Attempts        uint64 `json:"attempts"`
	FailedAttempts  uint64 `json:"failed_attempts"`
}

type ModelUsage struct {
	BaseURL         string `json:"base_url" gorm:"column:base_url"`
	Model           string `json:"model"`
	InputTokens     uint64 `json:"input_tokens"`
	OutputTokens    uint64 `json:"output_tokens"`
	UnsplitTokens   uint64 `json:"unsplit_tokens"`
	EstimatedTokens uint64 `json:"estimated_tokens"`
}

type OverviewTask struct {
	ID         string  `json:"task_id"`
	Goal       string  `json:"goal"`
	Status     string  `json:"status"`
	Phase      *string `json:"phase,omitempty"`
	CreatedAt  string  `json:"created_at"`
	FinishedAt *string `json:"finished_at,omitempty"`
}

func (TaskSnapshot) TableName() string { return "research_tasks" }

type EventPosition struct {
	Status    string
	LatestSeq uint64
	MinSeq    uint64
}

type EventBatchRow struct {
	EventPosition
	Seq       *uint64
	Kind      string
	Payload   json.RawMessage
	Timestamp string
}

type TaskEvent struct {
	Seq       uint64
	Kind      string `gorm:"column:event_type"`
	Payload   json.RawMessage
	Timestamp string
}

type Source struct {
	ID      string `json:"source_id" gorm:"column:source_id"`
	Title   string `json:"title"`
	URL     string `json:"url" gorm:"column:canonical_url"`
	Excerpt string `json:"excerpt"`
	Type    string `json:"source_type,omitempty" gorm:"column:source_type"`
}

type ReportEvidence struct {
	Kind            string              `json:"kind"`
	ArchivedSources int                 `json:"archived_sources"`
	Citations       []CitationReference `json:"citations" gorm:"-"`
}

type ReportFeedback struct {
	Rating  string `json:"rating"`
	Comment string `json:"comment"`
}

type CitationReference struct {
	ID      string `json:"source_id" gorm:"column:source_id"`
	Title   string `json:"title"`
	Type    string `json:"source_type" gorm:"column:source_type"`
	Ordinal int    `json:"ordinal"`
}

type RequestKey struct {
	Digest []byte `gorm:"column:request_digest"`
	TaskID string
}

type BudgetAttempt struct {
	ReservedTokens uint32
	Status         string
}

type ExecutionTask struct {
	Status              string
	Goal                string
	ParentTaskID        *string
	CreationLogID       string `gorm:"column:creation_logid"`
	PreferencesSnapshot *string
	RequiresPlanReview  bool
	ReviewCitations     bool
	ApprovedPlan        *string
	Generation          uint32
	LeaseEpoch          uint32
	EventSeq            uint64
	PastDeadline        bool
	ValidLease          bool
	RetryCount          uint16
	RemainingSeconds    float64
	ModelVersion        uint32
}
