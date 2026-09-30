package model

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"emailtracker.com/db"
)

// ListCampaignSMTPAccountIDs returns the campaign's mailbox allowlist (may be empty = all seats).
func ListCampaignSMTPAccountIDs(campaignID int64) ([]int64, error) {
	rows, err := db.Query(`
		SELECT smtp_account_id FROM campaign_smtp_accounts
		WHERE campaign_id = ?
		ORDER BY smtp_account_id ASC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// CampaignHasSMTPAllowlist is true when the campaign has at least one explicit seat.
func CampaignHasSMTPAllowlist(campaignID int64) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM campaign_smtp_accounts WHERE campaign_id = ?`, campaignID).Scan(&n)
	return n > 0
}

// SetCampaignSMTPAccounts replaces the campaign mailbox allowlist.
// Empty smtpIDs clears the allowlist (meaning: use all ready seats).
func SetCampaignSMTPAccounts(campaignID, userID int64, smtpIDs []int64) error {
	if _, err := GetCampaignForUser(campaignID, userID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	var clean []int64
	for _, id := range smtpIDs {
		if id <= 0 || seen[id] {
			continue
		}
		acc, err := GetSMTPAccount(id)
		if err != nil || acc.UserID != userID {
			return fmt.Errorf("mailbox not found")
		}
		seen[id] = true
		clean = append(clean, id)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM campaign_smtp_accounts WHERE campaign_id = ?`, campaignID); err != nil {
		return err
	}
	for _, id := range clean {
		if _, err := tx.Exec(`
			INSERT INTO campaign_smtp_accounts (campaign_id, smtp_account_id) VALUES (?, ?)
			ON CONFLICT DO NOTHING
		`, campaignID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSendReadyAccountsForCampaign returns ready seats, optionally filtered by campaign allowlist.
// Empty allowlist → all ready seats (legacy / “use everything”).
func ListSendReadyAccountsForCampaign(userID, campaignID int64) ([]SMTPAccount, error) {
	ready, err := ListSendReadyAccountsForUser(userID)
	if err != nil || campaignID <= 0 || len(ready) == 0 {
		return ready, err
	}
	allowed, err := ListCampaignSMTPAccountIDs(campaignID)
	if err != nil {
		return ready, err
	}
	if len(allowed) == 0 {
		return ready, nil
	}
	allow := make(map[int64]bool, len(allowed))
	for _, id := range allowed {
		allow[id] = true
	}
	var out []SMTPAccount
	for _, acc := range ready {
		if allow[acc.ID] {
			out = append(out, acc)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ready mailboxes selected for this campaign — pick seats on the campaign page")
	}
	return out, nil
}

// ParseSMTPAccountIDsForm parses repeated smtp_account_ids form values.
func ParseSMTPAccountIDsForm(values []string) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, raw := range values {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// CampaignSMTPOption is one seat shown on the campaign mailbox picker.
type CampaignSMTPOption struct {
	Account  SMTPAccount
	Selected bool
	Ready    bool
}

// CampaignSMTPSelection is ready/active seats plus which are selected for a campaign.
type CampaignSMTPSelection struct {
	Accounts       []SMTPAccount // all active seats shown in the picker
	Options        []CampaignSMTPOption
	SelectedIDs    map[int64]bool
	AllowlistEmpty bool // true → campaign uses all ready seats
}

// GetCampaignSMTPSelection loads active mailboxes and the campaign allowlist for UI.
func GetCampaignSMTPSelection(userID, campaignID int64) (CampaignSMTPSelection, error) {
	out := CampaignSMTPSelection{SelectedIDs: map[int64]bool{}}
	all, err := MapSMTPAccountsByID(userID)
	if err != nil {
		return out, err
	}
	var accounts []SMTPAccount
	for _, a := range all {
		if a.Status != "active" {
			continue
		}
		_ = EnsureDailyCounterReset(a.ID)
		if fresh, fErr := GetSMTPAccount(a.ID); fErr == nil {
			a = fresh
		}
		accounts = append(accounts, a)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	out.Accounts = accounts

	allowed, err := ListCampaignSMTPAccountIDs(campaignID)
	if err != nil {
		return out, err
	}
	if campaignID <= 0 || len(allowed) == 0 {
		out.AllowlistEmpty = true
		for _, a := range accounts {
			out.SelectedIDs[a.ID] = true
		}
	} else {
		for _, id := range allowed {
			out.SelectedIDs[id] = true
		}
	}
	for _, a := range accounts {
		out.Options = append(out.Options, CampaignSMTPOption{
			Account:  a,
			Selected: out.SelectedIDs[a.ID],
			Ready:    a.IsSendReady(),
		})
	}
	return out, nil
}
