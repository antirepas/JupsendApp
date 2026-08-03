package model

import (
	"fmt"
	"time"

	"emailtracker.com/db"
)

// IsSendReady reports whether this profile can send mail (InboxKit SMTP, Gmail OAuth, or legacy SMTP).
func (a SMTPAccount) IsSendReady() bool {
	if a.Status != "active" {
		return false
	}
	if a.IsGoogleOAuth() {
		return a.GoogleEmail != "" && a.SMTPHost != ""
	}
	if a.MailboxSource == "inboxkit" || a.MailboxSource == MailboxSourceShared || a.MailboxSource == MailboxSourceManual {
		return a.SMTPHost != "" && a.SMTPUser != "" && a.SMTPPassword != ""
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
		return SMTPAccount{}, fmt.Errorf("no sending mailbox — finish domain setup under Mailboxes")
	}
	_ = EnsureDailyCounterReset(acc.ID)
	acc, err = GetSMTPAccount(acc.ID)
	if err != nil {
		return SMTPAccount{}, err
	}
	if !acc.IsSendReady() {
		if acc.MailboxSource == "inboxkit" {
			return SMTPAccount{}, fmt.Errorf("mailbox not ready yet — check Mailboxes setup status")
		}
		if acc.MailboxSource == MailboxSourceShared {
			return SMTPAccount{}, fmt.Errorf("shared sending mailbox not configured — contact support")
		}
		if acc.AuthType == AuthTypeGoogleOAuth || acc.GoogleEmail != "" {
			return SMTPAccount{}, fmt.Errorf("gmail connection incomplete — reconnect Gmail in Settings")
		}
		return SMTPAccount{}, fmt.Errorf("no ready sending mailbox — open Mailboxes to finish setup")
	}
	return acc, nil
}
