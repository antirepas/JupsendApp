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

// smtpAccountIDsLinkedToOutreach returns smtp_account_id values attached to the user's outreach mailboxes.
func smtpAccountIDsLinkedToOutreach(userID int64) (map[int64]bool, error) {
	rows, err := db.Query(`
		SELECT DISTINCT smtp_account_id FROM outreach_mailboxes
		WHERE user_id = ? AND COALESCE(smtp_account_id, 0) > 0
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out[id] = true
	}
	return out, nil
}

// filterSendReadySeats drops Free shared seats when personal seats exist, and when the user
// has outreach-linked SMTP rows, keeps only those (so orphan/legacy profiles don't inflate warmup).
func filterSendReadySeats(userID int64, ready []SMTPAccount) []SMTPAccount {
	if len(ready) == 0 {
		return ready
	}
	hasPersonal := false
	for _, a := range ready {
		if a.MailboxSource != MailboxSourceShared {
			hasPersonal = true
			break
		}
	}
	if hasPersonal {
		var personal []SMTPAccount
		for _, a := range ready {
			if a.MailboxSource != MailboxSourceShared {
				personal = append(personal, a)
			}
		}
		ready = personal
	}

	linked, err := smtpAccountIDsLinkedToOutreach(userID)
	if err != nil || len(linked) == 0 {
		return ready
	}
	var matched []SMTPAccount
	for _, a := range ready {
		if linked[a.ID] {
			matched = append(matched, a)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return ready
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
	ready = filterSendReadySeats(userID, ready)
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
