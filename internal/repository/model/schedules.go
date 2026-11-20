package model

type ResearchSchedule struct {
	ID            string  `json:"schedule_id"`
	Goal          string  `json:"goal"`
	IntervalDays  int     `json:"interval_days"`
	Status        string  `json:"status"`
	NextRun       *string `json:"next_run,omitempty"`
	LastTaskID    *string `json:"last_task_id,omitempty"`
	NotifyResults bool    `json:"notify_results"`
}

type DueSchedule struct {
	TenantID string
	ID       string
}

type ScheduleResult struct {
	TaskID     string `json:"task_id"`
	Goal       string `json:"goal"`
	Status     string `json:"status"`
	FinishedAt string `json:"finished_at"`
	Unread     bool   `json:"unread"`
}
