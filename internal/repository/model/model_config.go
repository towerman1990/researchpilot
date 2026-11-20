package model

type ModelSummary struct {
	Version uint32
	BaseURL string `gorm:"column:base_url"`
	Model   string
	KeyTail string
	Revoked bool
}

type ModelProfile struct {
	ID       uint32 `json:"profile_id"`
	Label    string `json:"label"`
	IsActive bool   `json:"active"`
	Version  uint32 `json:"version"`
	BaseURL  string `json:"base_url" gorm:"column:base_url"`
	Model    string `json:"model"`
	KeyTail  string `json:"key_tail"`
}

func (ModelSummary) TableName() string { return "model_configs" }
