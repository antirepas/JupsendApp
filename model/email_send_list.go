package model

import (
	"fmt"
	"math"
	"strings"

	"emailtracker.com/db"
)

const CancelledCampaignStopMsg = "cancelled: campaign stopped"

// IsCancelledSend reports whether a send/job pair is an intentional cancel (not a real delivery failure).
func IsCancelledSend(deliveryStatus, jobStatus, lastError string) bool {
	ds := strings.ToLower(strings.TrimSpace(deliveryStatus))
	js := strings.ToLower(strings.TrimSpace(jobStatus))
	if ds == "cancelled" || js == "cancelled" {
		return true
	}
	err := strings.ToLower(strings.TrimSpace(lastError))
	return strings.Contains(err, "cancelled: campaign stopped")
}

func (i EmailSendListItem) IsCancelled() bool {
	return IsCancelledSend(i.DeliveryStatus, i.JobStatus, i.DeliveryError)
}

// CanDelete is true for never-delivered rows (safe to purge without losing analytics).
func (i EmailSendListItem) CanDelete() bool {
	ds := strings.ToLower(strings.TrimSpace(i.DeliveryStatus))
	if ds == "sent" {
		return false
	}
	return ds == "queued" || ds == "sending" || ds == "failed" || ds == "cancelled" || ds == "unknown" || ds == ""
}

type SendListFilter struct {
	Status     string // "", queued, sent, failed, cancelled, opened, clicked, all
	CampaignID int64
	Query      string
	Page       int
	PageSize   int
}

type SendListPage struct {
	Items      []EmailSendListItem
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

type SendListCounts struct {
	Queued    int
	SentToday int
	Failed    int
	Cancelled int
}

func cancelledSendSQL(aliasES, aliasSJ string) string {
	return fmt.Sprintf(`(
		LOWER(COALESCE(%s.delivery_status, '')) = 'cancelled'
		OR LOWER(COALESCE(%s.status, '')) = 'cancelled'
		OR (
			LOWER(COALESCE(%s.delivery_status, '')) = 'failed'
			AND LOWER(COALESCE(%s.last_error, '')) LIKE '%%cancelled: campaign stopped%%'
		)
	)`, aliasES, aliasSJ, aliasES, aliasSJ)
}

func buildSendListWhere(userID int64, f SendListFilter) (string, []interface{}) {
	where := []string{"es.user_id = ?"}
	args := []interface{}{userID}

	status := strings.ToLower(strings.TrimSpace(f.Status))
	cancelled := cancelledSendSQL("es", "sj")

	switch status {
	case "all":
		// no status filter
	case "cancelled":
		where = append(where, cancelled)
	case "queued":
		where = append(where, `LOWER(COALESCE(es.delivery_status, '')) IN ('queued', 'sending')`)
		where = append(where, "NOT "+cancelled)
	case "sent":
		where = append(where, `LOWER(COALESCE(es.delivery_status, '')) = 'sent'`)
	case "failed":
		where = append(where, `LOWER(COALESCE(es.delivery_status, '')) = 'failed'`)
		where = append(where, "NOT "+cancelled)
	case "opened":
		where = append(where, `LOWER(COALESCE(es.delivery_status, '')) = 'sent'`)
		where = append(where, `EXISTS (
			SELECT 1 FROM email_events ee2
			WHERE (ee2.email_send_id = es.id OR ee2.tracking_id = es.tracking_id) AND ee2.event_type = 'open'
		)`)
	case "clicked":
		where = append(where, `EXISTS (
			SELECT 1 FROM email_events ee2
			WHERE (ee2.email_send_id = es.id OR ee2.tracking_id = es.tracking_id) AND ee2.event_type = 'click'
		)`)
	default:
		// Default inbox: hide cancelled noise.
		where = append(where, "NOT "+cancelled)
	}

	if f.CampaignID > 0 {
		where = append(where, "es.campaign_id = ?")
		args = append(args, f.CampaignID)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		where = append(where, `(LOWER(COALESCE(c.email, '')) LIKE ? OR LOWER(COALESCE(t.name, '')) LIKE ? OR LOWER(COALESCE(t.subject, '')) LIKE ? OR LOWER(COALESCE(camp.name, '')) LIKE ?)`)
		args = append(args, like, like, like, like)
	}

	return strings.Join(where, " AND "), args
}

func ListEmailSendsFiltered(userID int64, f SendListFilter) (SendListPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 25
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}

	whereSQL, args := buildSendListWhere(userID, f)

	fromSQL := `
		FROM email_sends es
		LEFT JOIN template t ON t.id = es.template_id
		LEFT JOIN contact c ON c.id = es.contact_id
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		LEFT JOIN campaigns camp ON camp.id = es.campaign_id
	`

	var total int
	countQ := `SELECT COUNT(DISTINCT es.id) ` + fromSQL + ` WHERE ` + whereSQL
	if err := db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return SendListPage{}, err
	}

	offset := (f.Page - 1) * f.PageSize
	listQ := `
		SELECT
			es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at,
			COALESCE(t.name, ''), COALESCE(NULLIF(es.rendered_subject, ''), COALESCE(t.subject, '')), COALESCE(c.email, ''),
			COALESCE(NULLIF(sa.google_email, ''), NULLIF(sa.from_email, ''), NULLIF(sa.smtp_user, ''), ''),
			COALESCE(SUM(CASE WHEN ee.event_type = 'open' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ee.event_type = 'click' THEN 1 ELSE 0 END), 0),
			COALESCE(NULLIF(es.delivery_status, ''), 'unknown'),
			COALESCE(sj.last_error, ''),
			COALESCE(sj.status, ''),
			COALESCE(es.campaign_id, 0), COALESCE(camp.name, ''),
			COALESCE(es.rendered_subject, ''), COALESCE(es.rendered_html, ''), COALESCE(es.rendered_text, ''),
			COALESCE(es.smtp_account_id, 0)
		` + fromSQL + `
		LEFT JOIN smtp_accounts sa ON sa.id = es.smtp_account_id OR (es.smtp_account_id IS NULL AND sa.user_id = es.user_id AND sa.is_default = 1)
		LEFT JOIN email_events ee ON ee.email_send_id = es.id OR ee.tracking_id = es.tracking_id
		WHERE ` + whereSQL + `
		GROUP BY es.id, es.template_id, es.contact_id, es.tracking_id, es.sent_at, es.delivery_status, es.campaign_id,
			es.rendered_subject, es.rendered_html, es.rendered_text, es.smtp_account_id,
			t.name, t.subject, c.email, sa.google_email, sa.from_email, sa.smtp_user, sj.last_error, sj.status, camp.name
		ORDER BY es.id DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), f.PageSize, offset)
	rows, err := db.Query(listQ, listArgs...)
	if err != nil {
		return SendListPage{}, err
	}
	defer rows.Close()

	var items []EmailSendListItem
	for rows.Next() {
		item, err := scanEmailSendListItem(rows.Scan)
		if err != nil {
			return SendListPage{}, err
		}
		items = append(items, item)
	}

	totalPages := int(math.Ceil(float64(total) / float64(f.PageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	return SendListPage{
		Items:      items,
		Total:      total,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalPages: totalPages,
	}, nil
}

func CountEmailSendsSummary(userID int64) (SendListCounts, error) {
	var c SendListCounts
	cancelled := cancelledSendSQL("es", "sj")
	from := `
		FROM email_sends es
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ?
	`
	err := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(es.delivery_status,'')) IN ('queued','sending') AND NOT `+cancelled+` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(es.delivery_status,'')) = 'sent'
				AND es.sent_at IS NOT NULL AND es.sent_at >= CURRENT_DATE THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(es.delivery_status,'')) = 'failed' AND NOT `+cancelled+` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN `+cancelled+` THEN 1 ELSE 0 END), 0)
		`+from, userID).Scan(&c.Queued, &c.SentToday, &c.Failed, &c.Cancelled)
	if err != nil {
		return c, err
	}
	return c, nil
}

// deleteEmailSendIDsTx removes never-delivered sends and related queue/link rows.
func deleteEmailSendIDsTx(tx *db.Tx, userID int64, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, userID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inList := strings.Join(placeholders, ",")

	// Only never-delivered rows owned by this user.
	rows, err := tx.Query(`
		SELECT id FROM email_sends
		WHERE user_id = ? AND id IN (`+inList+`)
			AND LOWER(COALESCE(delivery_status, '')) <> 'sent'
	`, args...)
	if err != nil {
		return 0, err
	}
	var deletable []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		deletable = append(deletable, id)
	}
	rows.Close()
	if len(deletable) == 0 {
		return 0, nil
	}

	ph2 := make([]string, len(deletable))
	args2 := make([]interface{}, len(deletable))
	for i, id := range deletable {
		ph2[i] = "?"
		args2[i] = id
	}
	in2 := strings.Join(ph2, ",")

	if _, err := tx.Exec(`DELETE FROM send_jobs WHERE email_send_id IN (`+in2+`)`, args2...); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM tracked_links WHERE email_send_id IN (`+in2+`)`, args2...); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE contact_events SET email_send_id = NULL WHERE email_send_id IN (`+in2+`)`, args2...); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM email_sends WHERE id IN (`+in2+`)`, args2...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteEmailSendsForUser deletes never-delivered sends owned by the user.
func DeleteEmailSendsForUser(userID int64, ids []int64) (int, error) {
	if userID <= 0 || len(ids) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := deleteEmailSendIDsTx(tx, userID, ids)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// ClearCancelledSends removes cancelled / campaign-stop leftovers for a user.
func ClearCancelledSends(userID int64) (int, error) {
	rows, err := db.Query(`
		SELECT es.id
		FROM email_sends es
		LEFT JOIN send_jobs sj ON sj.id = es.send_job_id
		WHERE es.user_id = ? AND `+cancelledSendSQL("es", "sj")+`
			AND LOWER(COALESCE(es.delivery_status, '')) <> 'sent'
	`, userID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	return DeleteEmailSendsForUser(userID, ids)
}

// DeleteEmailSendForUser deletes one never-delivered send.
func DeleteEmailSendForUser(userID, sendID int64) (bool, error) {
	n, err := DeleteEmailSendsForUser(userID, []int64{sendID})
	return n > 0, err
}