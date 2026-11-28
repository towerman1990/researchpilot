package model

type Conversation struct {
	ID         string `json:"conversation_id"`
	Title      string `json:"title"`
	Turns      uint64 `json:"turns" gorm:"column:next_turn"`
	LastTaskID string `json:"last_task_id"`
	Status     string `json:"status"`
	UpdatedAt  string `json:"updated_at"`
}
