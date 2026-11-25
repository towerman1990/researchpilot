package repository

import (
	"context"
	"crypto/sha256"
	"time"
)

func guestTrialSubjects(tenant, address string) [2][32]byte {
	return [2][32]byte{sha256.Sum256([]byte("address:" + address)), sha256.Sum256([]byte("guest:" + tenant))}
}

func guestTrialDay(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.DateOnly)
}

func (s *Store) GuestTrialUsed(ctx context.Context, tenant, address string) (int, error) {
	subjects := guestTrialSubjects(tenant, address)
	var used int
	err := s.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(used), 0) FROM guest_trial_usage WHERE trial_day = ? AND (subject = ? OR subject = ?)",
		guestTrialDay(time.Now()), subjects[0][:], subjects[1][:]).Scan(&used).Error
	return used, err
}

func (t WriteTx) ConsumeGuestTrial(tenant, address string, limit int) (bool, error) {
	day := guestTrialDay(time.Now())
	for _, subject := range guestTrialSubjects(tenant, address) {
		if err := t.db.Exec("INSERT INTO guest_trial_usage (trial_day, subject) VALUES (?, ?) ON DUPLICATE KEY UPDATE used = used",
			day, subject[:]).Error; err != nil {
			return false, err
		}
		result := t.db.Exec("UPDATE guest_trial_usage SET used = used + 1 WHERE trial_day = ? AND subject = ? AND used < ?",
			day, subject[:], limit)
		if result.Error != nil || result.RowsAffected == 0 {
			return false, result.Error
		}
	}
	return true, nil
}

func (t WriteTx) EnsureGuestUser(tenant string) error {
	return t.db.Exec("INSERT INTO users (id, email, password_hash) VALUES (?, ?, '') ON DUPLICATE KEY UPDATE id = id",
		tenant, "guest:"+tenant).Error
}
