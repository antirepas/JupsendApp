package model

import (
	"fmt"
	"strings"

	"emailtracker.com/db"
)

// CampaignMailboxSeat is one seat's share of a campaign's contacts.
type CampaignMailboxSeat struct {
	SMTPAccountID int64
	Email         string
	FromName      string
	PlannedCount  int
	SentCount     int
}

// CampaignMailboxDistribution is sticky seat assignment + actual send counts.
type CampaignMailboxDistribution struct {
	Seats      []CampaignMailboxSeat
	TotalPlan  int
	TotalSent  int
	SeatCount  int
	HasSeats   bool
}

// CampaignMailboxDistribution builds planned (sticky) and sent counts per mailbox for a campaign.
func GetCampaignMailboxDistribution(userID, campaignID int64) (CampaignMailboxDistribution, error) {
	out := CampaignMailboxDistribution{}
	ready, err := ListSendReadyAccountsForUser(userID)
	if err != nil || len(ready) == 0 {
		return out, nil
	}
	out.HasSeats = true
	out.SeatCount = len(ready)

	byID := make(map[int64]*CampaignMailboxSeat, len(ready))
	for _, acc := range ready {
		email := acc.SenderEmail()
		seat := &CampaignMailboxSeat{
			SMTPAccountID: acc.ID,
			Email:         email,
			FromName:      strings.TrimSpace(acc.FromName),
		}
		byID[acc.ID] = seat
		out.Seats = append(out.Seats, *seat)
	}
	// Re-point map to slice elements after append.
	byID = make(map[int64]*CampaignMailboxSeat, len(out.Seats))
	for i := range out.Seats {
		byID[out.Seats[i].SMTPAccountID] = &out.Seats[i]
	}

	contactIDs, err := GetCampaignContactIDs(campaignID)
	if err != nil {
		return out, err
	}
	out.TotalPlan = len(contactIDs)
	for _, cid := range contactIDs {
		accID := stickySMTPAccountID(userID, cid, ready)
		if seat, ok := byID[accID]; ok {
			seat.PlannedCount++
		}
	}

	rows, err := db.Query(`
		SELECT COALESCE(smtp_account_id, 0), COUNT(*)
		FROM email_sends
		WHERE user_id = ? AND campaign_id = ? AND delivery_status = 'sent' AND COALESCE(smtp_account_id,0) > 0
		GROUP BY smtp_account_id
	`, userID, campaignID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var smtpID int64
		var n int
		if err := rows.Scan(&smtpID, &n); err != nil {
			continue
		}
		out.TotalSent += n
		if seat, ok := byID[smtpID]; ok {
			seat.SentCount = n
		} else {
			// Sent from a seat no longer ready — still show it.
			label := fmt.Sprintf("mailbox #%d", smtpID)
			if acc, aErr := GetSMTPAccount(smtpID); aErr == nil {
				label = acc.SenderEmail()
			}
			out.Seats = append(out.Seats, CampaignMailboxSeat{
				SMTPAccountID: smtpID,
				Email:         label,
				SentCount:     n,
			})
		}
	}
	return out, nil
}

func stickySMTPAccountID(userID, contactID int64, ready []SMTPAccount) int64 {
	if len(ready) == 0 {
		return 0
	}
	byID := make(map[int64]SMTPAccount, len(ready))
	for _, acc := range ready {
		byID[acc.ID] = acc
	}
	if contactID > 0 {
		if lastID, err := LatestSMTPAccountForContact(userID, contactID); err == nil && lastID > 0 {
			if _, ok := byID[lastID]; ok {
				return lastID
			}
		}
		idx := int(contactID % int64(len(ready)))
		if idx < 0 {
			idx = 0
		}
		return ready[idx].ID
	}
	return ready[0].ID
}

// PlannedPct returns planned share of total contacts (0–100).
func (s CampaignMailboxSeat) PlannedPct(total int) int {
	if total <= 0 || s.PlannedCount <= 0 {
		return 0
	}
	return (s.PlannedCount * 100) / total
}
