package model

type ModelSummary struct {
	Version uint32
	BaseURL string `gorm:"column:base_url"`
	Model   string
	KeyTail string
	Revoked bool
}

func (ModelSummary) TableName() string { return "model_configs" }
