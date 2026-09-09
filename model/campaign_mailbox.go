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

// GetCampaignMailboxDistribution builds planned (sticky) and sent counts per mailbox for a campaign.
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

	// One batched sticky lookup — never N× LatestSMTPAccountForContact (that 502s large lists).
	stickyByContact, _ := latestSMTPAccountsForCampaignContacts(userID, campaignID)
	readySet := make(map[int64]struct{}, len(ready))
	for _, acc := range ready {
		readySet[acc.ID] = struct{}{}
	}
	for _, cid := range contactIDs {
		accID := stickySMTPAccountIDCached(cid, ready, readySet, stickyByContact)
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

// latestSMTPAccountsForCampaignContacts returns contact_id → last sticky smtp_account_id
// for contacts on this campaign (same sources as LatestSMTPAccountForContact, batched).
func latestSMTPAccountsForCampaignContacts(userID, campaignID int64) (map[int64]int64, error) {
	out := make(map[int64]int64)
	rows, err := db.Query(`
		SELECT DISTINCT ON (contact_id) contact_id, smtp_account_id
		FROM (
			SELECT es.contact_id,
				COALESCE(es.smtp_account_id, 0) AS smtp_account_id,
				COALESCE(es.sent_at, TIMESTAMPTZ 'epoch') AS ts,
				es.id
			FROM email_sends es
			INNER JOIN campaign_contacts cc ON cc.contact_id = es.contact_id AND cc.campaign_id = ?
			WHERE es.user_id = ? AND COALESCE(es.smtp_account_id, 0) > 0
			  AND es.delivery_status IN ('sent', 'sending')
			UNION ALL
			SELECT cm.contact_id,
				COALESCE(cm.smtp_account_id, 0),
				cm.occurred_at,
				cm.id
			FROM conversation_messages cm
			INNER JOIN campaign_contacts cc ON cc.contact_id = cm.contact_id AND cc.campaign_id = ?
			WHERE cm.user_id = ? AND cm.direction = 'outbound'
			  AND COALESCE(cm.smtp_account_id, 0) > 0
			UNION ALL
			SELECT sj.contact_id,
				COALESCE(sj.smtp_account_id, 0),
				COALESCE(sj.updated_at, sj.created_at),
				sj.id
			FROM send_jobs sj
			INNER JOIN campaign_contacts cc ON cc.contact_id = sj.contact_id AND cc.campaign_id = ?
			WHERE sj.user_id = ? AND sj.status IN ('sent', 'processing', 'pending')
			  AND COALESCE(sj.smtp_account_id, 0) > 0
		) t
		ORDER BY contact_id, ts DESC NULLS LAST, id DESC
	`, campaignID, userID, campaignID, userID, campaignID, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var contactID, smtpID int64
		if err := rows.Scan(&contactID, &smtpID); err != nil {
			continue
		}
		if smtpID > 0 {
			out[contactID] = smtpID
		}
	}
	return out, nil
}

func stickySMTPAccountID(userID, contactID int64, ready []SMTPAccount) int64 {
	if len(ready) == 0 {
		return 0
	}
	readySet := make(map[int64]struct{}, len(ready))
	for _, acc := range ready {
		readySet[acc.ID] = struct{}{}
	}
	var sticky map[int64]int64
	if contactID > 0 {
		if lastID, err := LatestSMTPAccountForContact(userID, contactID); err == nil && lastID > 0 {
			sticky = map[int64]int64{contactID: lastID}
		}
	}
	return stickySMTPAccountIDCached(contactID, ready, readySet, sticky)
}

func stickySMTPAccountIDCached(contactID int64, ready []SMTPAccount, readySet map[int64]struct{}, stickyByContact map[int64]int64) int64 {
	if len(ready) == 0 {
		return 0
	}
	if contactID > 0 {
		if lastID := stickyByContact[contactID]; lastID > 0 {
			if _, ok := readySet[lastID]; ok {
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
