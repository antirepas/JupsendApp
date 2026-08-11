package model

import (
	"fmt"
	"sort"
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
	ready, err := ListSendReadyAccountsForUser(userID)
	if err != nil {
		return SMTPAccount{}, err
	}
	if len(ready) == 0 {
		return SMTPAccount{}, fmt.Errorf("no ready sending mailbox — open Mailboxes to finish setup")
	}
	for _, acc := range ready {
		if acc.IsDefault {
			return acc, nil
		}
	}
	return ready[0], nil
}

// ListSendReadyAccountsForUser returns all active SMTP accounts that can send, ordered by id.
func ListSendReadyAccountsForUser(userID int64) ([]SMTPAccount, error) {
	all, err := MapSMTPAccountsByID(userID)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no sending mailbox — finish domain setup under Mailboxes")
	}
	var ready []SMTPAccount
	for _, acc := range all {
		_ = EnsureDailyCounterReset(acc.ID)
		fresh, err := GetSMTPAccount(acc.ID)
		if err != nil {
			continue
		}
		if fresh.IsSendReady() {
			ready = append(ready, fresh)
		}
	}
	if len(ready) == 0 {
		pref, prefErr := GetSMTPAccountByUserID(userID)
		if prefErr == nil {
			if pref.MailboxSource == "inboxkit" {
				return nil, fmt.Errorf("mailbox not ready yet — check Mailboxes setup status")
			}
			if pref.MailboxSource == MailboxSourceShared {
				return nil, fmt.Errorf("shared sending mailbox not configured — contact support")
			}
			if pref.AuthType == AuthTypeGoogleOAuth || pref.GoogleEmail != "" {
				return nil, fmt.Errorf("gmail connection incomplete — reconnect Gmail in Settings")
			}
		}
		return nil, fmt.Errorf("no ready sending mailbox — open Mailboxes to finish setup")
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
	return ready, nil
}
