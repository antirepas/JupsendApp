package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

const (
	ImportKindContactsPaste       = "contacts_paste"
	ImportKindContactsUpload      = "contacts_upload"
	ImportKindCampaignPaste       = "campaign_paste"
	ImportKindCampaignUpload      = "campaign_upload"
	ImportKindCampaignListSnapshot = "campaign_list_snapshot"

	ImportStatusPending    = "pending"
	ImportStatusProcessing = "processing"
	ImportStatusDone       = "done"
	ImportStatusFailed     = "failed"

	MaxImportRows = 25000
)

// ImportJobPayload is stored as JSON on import_jobs.payload_json.
type ImportJobPayload struct {
	Rows           []ImportContactRow `json:"rows,omitempty"`
	ImportKeys     []string           `json:"import_keys,omitempty"`
	ListID         int64              `json:"list_id,omitempty"`
	CampaignID     int64              `json:"campaign_id,omitempty"`
	SnapshotListID int64              `json:"snapshot_list_id,omitempty"`
}

type ImportJob struct {
	ID             int64
	UserID         int64
	Kind           string
	Status         string
	ListID         int64
	CampaignID     int64
	PayloadJSON    string
	TotalRows      int
	ProcessedRows  int
	CreatedCount   int
	UpdatedCount   int
	SkippedCount   int
	ErrorCount     int
	Message        string
	ErrorMessage   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

func (j ImportJob) ProgressPercent() int {
	if j.TotalRows <= 0 {
		if j.Status == ImportStatusDone {
			return 100
		}
		return 0
	}
	p := (j.ProcessedRows * 100) / j.TotalRows
	if p > 100 {
		return 100
	}
	if p < 0 {
		return 0
	}
	return p
}

func (j ImportJob) KindLabel() string {
	switch j.Kind {
	case ImportKindContactsPaste:
		return "Paste import"
	case ImportKindContactsUpload:
		return "File import"
	case ImportKindCampaignPaste:
		return "Campaign paste import"
	case ImportKindCampaignUpload:
		return "Campaign file import"
	case ImportKindCampaignListSnapshot:
		return "Add list to campaign"
	default:
		return "Import"
	}
}

func EncodeImportPayload(p ImportJobPayload) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func DecodeImportPayload(raw string) (ImportJobPayload, error) {
	var p ImportJobPayload
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	err := json.Unmarshal([]byte(raw), &p)
	return p, err
}

// EnqueueImportJob creates a pending import job. Call NotifyImportWorker after.
func EnqueueImportJob(userID int64, kind string, payload ImportJobPayload) (ImportJob, error) {
	if len(payload.Rows) > MaxImportRows {
		return ImportJob{}, fmt.Errorf("too many rows (%d); max is %d", len(payload.Rows), MaxImportRows)
	}
	total := len(payload.Rows)
	if kind == ImportKindCampaignListSnapshot {
		ids, err := ListMemberContactIDs(payload.SnapshotListID, userID)
		if err != nil {
			return ImportJob{}, err
		}
		total = len(ids)
	}
	raw, err := EncodeImportPayload(payload)
	if err != nil {
		return ImportJob{}, err
	}
	now := time.Now()
	var id int64
	err = db.QueryRow(`
		INSERT INTO import_jobs (
			user_id, kind, status, list_id, campaign_id, payload_json, total_rows, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, userID, kind, ImportStatusPending, payload.ListID, payload.CampaignID, raw, total, now, now).Scan(&id)
	if err != nil {
		return ImportJob{}, err
	}
	return GetImportJob(id)
}

func GetImportJob(id int64) (ImportJob, error) {
	row := db.QueryRow(`
		SELECT id, user_id, kind, status, COALESCE(list_id,0), COALESCE(campaign_id,0), payload_json,
			total_rows, processed_rows, created_count, updated_count, skipped_count, error_count,
			COALESCE(message,''), COALESCE(error_message,''), created_at, updated_at, started_at, finished_at
		FROM import_jobs WHERE id = ?
	`, id)
	return scanImportJob(row)
}

func GetImportJobForUser(id, userID int64) (ImportJob, error) {
	j, err := GetImportJob(id)
	if err != nil {
		return j, err
	}
	if j.UserID != userID {
		return ImportJob{}, errNotFound
	}
	return j, nil
}

func scanImportJob(row interface{ Scan(...interface{}) error }) (ImportJob, error) {
	var j ImportJob
	err := row.Scan(
		&j.ID, &j.UserID, &j.Kind, &j.Status, &j.ListID, &j.CampaignID, &j.PayloadJSON,
		&j.TotalRows, &j.ProcessedRows, &j.CreatedCount, &j.UpdatedCount, &j.SkippedCount, &j.ErrorCount,
		&j.Message, &j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.FinishedAt,
	)
	return j, err
}

// ClaimNextImportJob claims one pending job for processing.
func ClaimNextImportJob() (ImportJob, bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return ImportJob{}, false, err
	}
	defer tx.Rollback()

	row := tx.QueryRow(`
		SELECT id FROM import_jobs
		WHERE status = 'pending'
		ORDER BY id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`)
	var id int64
	if err := row.Scan(&id); err != nil {
		return ImportJob{}, false, nil
	}
	now := time.Now()
	_, err = tx.Exec(`
		UPDATE import_jobs SET status=?, started_at=COALESCE(started_at, ?), updated_at=?
		WHERE id=? AND status='pending'
	`, ImportStatusProcessing, now, now, id)
	if err != nil {
		return ImportJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ImportJob{}, false, err
	}
	j, err := GetImportJob(id)
	return j, true, err
}

func UpdateImportJobProgress(id int64, processed, created, updated, skipped, errors int) error {
	_, err := db.Exec(`
		UPDATE import_jobs SET
			processed_rows=?, created_count=?, updated_count=?, skipped_count=?, error_count=?, updated_at=?
		WHERE id=?
	`, processed, created, updated, skipped, errors, time.Now(), id)
	return err
}

func FinishImportJob(id int64, status, message, errMsg string, result ImportContactsResult, processed int) error {
	_, err := db.Exec(`
		UPDATE import_jobs SET
			status=?, message=?, error_message=?,
			processed_rows=?, created_count=?, updated_count=?, skipped_count=?, error_count=?,
			finished_at=?, updated_at=?,
			payload_json='{}'
		WHERE id=?
	`, status, message, errMsg, processed, result.Created, result.Updated, result.Skipped, result.Errors,
		time.Now(), time.Now(), id)
	return err
}

// ReclaimStaleImportJobs resets stuck processing jobs after restart.
func ReclaimStaleImportJobs() {
	_, _ = db.Exec(`
		UPDATE import_jobs SET status='pending', updated_at=?
		WHERE status='processing' AND updated_at < ?
	`, time.Now(), time.Now().Add(-30*time.Minute))
}

// ListActiveImportJobsForUser returns processing/pending jobs and recent finished ones.
func ListActiveImportJobsForUser(userID int64) ([]ImportJob, error) {
	rows, err := db.Query(`
		SELECT id, user_id, kind, status, COALESCE(list_id,0), COALESCE(campaign_id,0), payload_json,
			total_rows, processed_rows, created_count, updated_count, skipped_count, error_count,
			COALESCE(message,''), COALESCE(error_message,''), created_at, updated_at, started_at, finished_at
		FROM import_jobs
		WHERE user_id = ?
			AND (
				status IN ('pending', 'processing')
				OR (status IN ('done', 'failed') AND COALESCE(finished_at, updated_at) >= ?)
			)
		ORDER BY id DESC
		LIMIT 10
	`, userID, time.Now().Add(-30*time.Minute))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImportJob
	for rows.Next() {
		j, err := scanImportJob(rows)
		if err != nil {
			return out, err
		}
		// Don't send huge payloads to the client.
		j.PayloadJSON = ""
		out = append(out, j)
	}
	return out, nil
}

func DismissImportJob(id, userID int64) error {
	_, err := db.Exec(`
		UPDATE import_jobs SET finished_at = ?, updated_at = ?, message = COALESCE(NULLIF(message,''), 'Dismissed')
		WHERE id = ? AND user_id = ? AND status IN ('done', 'failed')
	`, time.Now().Add(-2*time.Hour), time.Now(), id, userID)
	return err
}
