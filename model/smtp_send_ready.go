package model

import (
	"fmt"
	"time"

	"emailtracker.com/db"
)

// IsSendReady reports whether this profile can send mail (active Gmail OAuth or legacy SMTP creds).
func (a SMTPAccount) IsSendReady() bool {
	if a.Status != "active" {
		return false
	}
	if a.IsGoogleOAuth() {
		return a.GoogleEmail != "" && a.SMTPHost != ""
	}
	return a.SMTPHost != "" && a.SMTPUser != "" && a.SMTPPassword != ""
}

func EnsureDailyCounterReset(accountID int64) error {
	today := time.Now().Format("2006-01-02")
	_, err := db.Exec(`
		UPDATE smtp_accounts SET sends_today = 0, sends_today_reset_at = ?
		WHERE id = ? AND (sends_today_reset_at IS NULL OR sends_today_reset_at::date < CURRENT_DATE)
	`, today, accountID)
	return err
}

func GetSendReadyAccountForUser(userID int64) (SMTPAccount, error) {
	acc, err := GetSMTPAccountByUserID(userID)
	if err != nil {
		return SMTPAccount{}, fmt.Errorf("no sending profile — connect Gmail in Settings")
	}
	_ = EnsureDailyCounterReset(acc.ID)
	acc, err = GetSMTPAccountByUserID(userID)
	if err != nil {
		return SMTPAccount{}, err
	}
	if !acc.IsSendReady() {
		if acc.AuthType == AuthTypeGoogleOAuth || acc.GoogleEmail != "" {
			return SMTPAccount{}, fmt.Errorf("gmail connection incomplete — reconnect Gmail in Settings")
		}
		return SMTPAccount{}, fmt.Errorf("connect Gmail in Settings before sending")
	}
	return acc, nil
}
