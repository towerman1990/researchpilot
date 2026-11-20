package model

type Delivery struct {
	Tenant     string `json:"tenant_id" gorm:"column:tenant_id"`
	Task       string `json:"task_id" gorm:"column:task_id"`
	Generation uint32 `json:"generation"`
}
type OutboxClaim struct {
	Delivery
	ClaimEpoch uint32
}

func (OutboxClaim) TableName() string { return "task_outbox" }
