package model

type SiteUsage struct {
	Day        string `json:"day"`
	TodayUsers uint64 `json:"today_users"`
	TodayUses  uint64 `json:"today_uses"`
	TotalUsers uint64 `json:"total_users"`
	TotalUses  uint64 `json:"total_uses"`
}
