package repository

import (
	"context"
	"crypto/sha256"
	"time"

	"researchpilot/internal/repository/model"
)

func (t WriteTx) RecordSiteResearchUse(task, tenant, guestAddress string) error {
	identity := "account:" + tenant
	if guestAddress != "" {
		identity = "address:" + guestAddress
	}
	subject := sha256.Sum256([]byte(identity))
	return t.db.Exec("INSERT INTO site_research_usage(task_id,usage_day,subject) VALUES(?,?,?)",
		task, guestTrialDay(time.Now()), subject[:]).Error
}

func (s *Store) SiteUsage(ctx context.Context, now time.Time) (model.SiteUsage, error) {
	day := guestTrialDay(now)
	var stats model.SiteUsage
	err := s.db.WithContext(ctx).Raw(
		"SELECT COUNT(*) AS total_uses, COUNT(DISTINCT subject) AS total_users, COALESCE(SUM(usage_day=?),0) AS today_uses, COUNT(DISTINCT CASE WHEN usage_day=? THEN subject END) AS today_users FROM site_research_usage",
		day, day).Scan(&stats).Error
	stats.Day = day
	return stats, err
}
