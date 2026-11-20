package model

type Credential struct {
	Version    uint32
	BaseURL    string `gorm:"column:base_url"`
	Model      string
	KeyCipher  []byte
	KeyVersion uint32
}

type RotationCredential struct {
	TenantID   string
	Version    uint32
	KeyVersion uint32
	KeyCipher  []byte
}
