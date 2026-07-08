package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"emailtracker.com/db"
)

type WorkflowInstance struct {
	ID                 int64
	WorkflowVersionID  int64
	ContactID          int64
	CampaignID         *int64
	ForkRootID         *int64
	BranchPriority    int
	CurrentNodeKey     string
	Status             string
	NextWakeAt         *time.Time
	WaitingForEvent    string
	StartedAt          time.Time
	CompletedAt        *time.Time
	ContextJSON        string
}

func CreateWorkflowInstance(versionID, contactID, campaignID int64, entryNodeKey string) (int64, error) {
	var camp interface{}
	if campaignID > 0 {
		camp = campaignID
	}
	row := db.QueryRow(`
		INSERT INTO workflow_instances (
			workflow_version_id, contact_id, campaign_id, current_node_key, status, context_json
		) VALUES (?, ?, ?, ?, 'active', '{}') RETURNING id
	`, versionID, contactID, camp, entryNodeKey)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func CreateForkedWorkflowInstance(versionID, contactID, campaignID int64, forkRootID int64, currentNodeKey string, contextJSON string, branchPriority int) (int64, error) {
	var camp interface{}
	if campaignID > 0 {
		camp = campaignID
	}
	if contextJSON == "" {
		contextJSON = "{}"
	}
	row := db.QueryRow(`
		INSERT INTO workflow_instances (
			workflow_version_id, contact_id, campaign_id, fork_root_id, branch_priority,
			current_node_key, status, context_json
		) VALUES (?, ?, ?, ?, ?, ?, 'active', ?) RETURNING id
	`, versionID, contactID, camp, forkRootID, branchPriority, currentNodeKey, contextJSON)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetWorkflowInstance(id int64) (WorkflowInstance, error) {
	row := db.QueryRow(`
		SELECT id, workflow_version_id, contact_id, campaign_id, fork_root_id, branch_priority,
			current_node_key, status, next_wake_at, waiting_for_event, started_at, completed_at, context_json
		FROM workflow_instances WHERE id = ?
	`, id)
	var inst WorkflowInstance
	var camp sqlNullInt64
	var forkRoot sqlNullInt64
	var branchPriority int
	var wake, completed sqlNullTime
	var waiting sql.NullString
	err := row.Scan(
		&inst.ID, &inst.WorkflowVersionID, &inst.ContactID, &camp, &forkRoot, &branchPriority,
		&inst.CurrentNodeKey, &inst.Status, &wake, &waiting, &inst.StartedAt, &completed, &inst.ContextJSON,
	)
	if camp.Valid {
		v := camp.Int64
		inst.CampaignID = &v
	}
	if forkRoot.Valid {
		v := forkRoot.Int64
		inst.ForkRootID = &v
	}
	inst.BranchPriority = branchPriority
	if wake.Valid {
		t := wake.Time
		inst.NextWakeAt = &t
	}
	if waiting.Valid {
		inst.WaitingForEvent = waiting.String
	}
	if completed.Valid {
		t := completed.Time
		inst.CompletedAt = &t
	}
	return inst, err
}

func ListInstancesForCampaign(campaignID int64) ([]WorkflowInstance, error) {
	rows, err := db.Query(`
		SELECT id, workflow_version_id, contact_id, campaign_id, fork_root_id, branch_priority,
			current_node_key, status, next_wake_at, waiting_for_event, started_at, completed_at, context_json
		FROM workflow_instances WHERE campaign_id = ?
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanInstances(rows)
}

func ListInstancesForWorkflow(workflowID int64) ([]WorkflowInstance, error) {
	rows, err := db.Query(`
		SELECT wi.id, wi.workflow_version_id, wi.contact_id, wi.campaign_id, wi.fork_root_id, wi.branch_priority,
			wi.current_node_key, wi.status,
			wi.next_wake_at, wi.waiting_for_event, wi.started_at, wi.completed_at, wi.context_json
		FROM workflow_instances wi
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ?
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanInstances(rows)
}

func scanInstances(rows interface{ Next() bool; Scan(...interface{}) error }) ([]WorkflowInstance, error) {
	var list []WorkflowInstance
	for rows.Next() {
		var inst WorkflowInstance
		var camp sqlNullInt64
		var forkRoot sqlNullInt64
		var branchPriority int
		var wake, completed sqlNullTime
		var waiting sql.NullString
		if err := rows.Scan(
			&inst.ID, &inst.WorkflowVersionID, &inst.ContactID, &camp, &forkRoot, &branchPriority,
			&inst.CurrentNodeKey, &inst.Status, &wake, &waiting, &inst.StartedAt, &completed, &inst.ContextJSON,
		); err != nil {
			return nil, err
		}
		if camp.Valid {
			v := camp.Int64
			inst.CampaignID = &v
		}
		if forkRoot.Valid {
			v := forkRoot.Int64
			inst.ForkRootID = &v
		}
		inst.BranchPriority = branchPriority
		if wake.Valid {
			t := wake.Time
			inst.NextWakeAt = &t
		}
		if waiting.Valid {
			inst.WaitingForEvent = waiting.String
		}
		if completed.Valid {
			t := completed.Time
			inst.CompletedAt = &t
		}
		list = append(list, inst)
	}
	return list, nil
}

func ClaimDueInstances(limit int) ([]int64, error) {
	token := uuid.New().String()
	expires := time.Now().Add(2 * time.Minute)
	now := time.Now()

	rows, err := db.Query(`
		SELECT id FROM workflow_instances
		WHERE status IN ('active', 'waiting')
		  AND (lock_expires_at IS NULL OR lock_expires_at < ?)
		  AND (
		    status = 'active' OR
		    (status = 'waiting' AND next_wake_at IS NOT NULL AND next_wake_at <= ?)
		  )
		ORDER BY branch_priority ASC, COALESCE(next_wake_at, started_at) ASC
		LIMIT ?
	`, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			res, err := db.Exec(`
				UPDATE workflow_instances SET lock_token = ?, lock_expires_at = ?
				WHERE id = ? AND (lock_expires_at IS NULL OR lock_expires_at < ?)
			`, token, expires, id, now)
			if err != nil {
				continue
			}
			if n, _ := res.RowsAffected(); n > 0 {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

func ClaimInstance(instanceID int64) (bool, error) {
	token := uuid.New().String()
	expires := time.Now().Add(2 * time.Minute)
	now := time.Now()
	res, err := db.Exec(`
		UPDATE workflow_instances SET lock_token = ?, lock_expires_at = ?
		WHERE id = ? AND status IN ('active', 'waiting')
		  AND (lock_expires_at IS NULL OR lock_expires_at < ?)
	`, token, expires, instanceID, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func ReleaseInstanceLock(instanceID int64) {
	_, _ = db.Exec(`UPDATE workflow_instances SET lock_token = NULL, lock_expires_at = NULL WHERE id = ?`, instanceID)
}

func UpdateInstanceState(inst WorkflowInstance) error {
	var wake, completed interface{}
	if inst.NextWakeAt != nil {
		wake = *inst.NextWakeAt
	}
	if inst.CompletedAt != nil {
		completed = *inst.CompletedAt
	}
	var waiting interface{}
	if inst.WaitingForEvent != "" {
		waiting = inst.WaitingForEvent
	}
	var forkRoot interface{}
	if inst.ForkRootID != nil {
		forkRoot = *inst.ForkRootID
	}
	_, err := db.Exec(`
		UPDATE workflow_instances SET
			fork_root_id = ?, branch_priority = ?, current_node_key = ?, status = ?, next_wake_at = ?, waiting_for_event = ?,
			completed_at = ?, context_json = ?, lock_token = NULL, lock_expires_at = NULL
		WHERE id = ?
	`, forkRoot, inst.BranchPriority, inst.CurrentNodeKey, inst.Status, wake, waiting, completed, inst.ContextJSON, inst.ID)
	return err
}

func WakeInstancesForContactEvent(contactID int64, eventType string) ([]int64, error) {
	rows, err := db.Query(`
		SELECT id FROM workflow_instances
		WHERE contact_id = ? AND status = 'waiting' AND waiting_for_event = ?
	`, contactID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	now := time.Now()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			_, _ = db.Exec(`
				UPDATE workflow_instances SET next_wake_at = ?, status = 'active', waiting_for_event = NULL WHERE id = ?
			`, now, id)
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func CreateExecution(instanceID int64, nodeKey, executionKey, status, outputJSON, errMsg string) (int64, error) {
	if outputJSON == "" {
		outputJSON = "{}"
	}
	row := db.QueryRow(`
		INSERT INTO workflow_executions (instance_id, node_key, execution_key, status, output_json, error_message, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(execution_key) DO NOTHING
		RETURNING id
	`, instanceID, nodeKey, executionKey, status, outputJSON, errMsg)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		err = db.QueryRow(`SELECT id FROM workflow_executions WHERE execution_key = ?`, executionKey).Scan(&id)
	}
	return id, err
}

func ExecutionExists(executionKey string) (bool, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM workflow_executions WHERE execution_key = ?`, executionKey).Scan(&id)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func GetInstanceContext(inst *WorkflowInstance) map[string]interface{} {
	m := map[string]interface{}{}
	_ = json.Unmarshal([]byte(inst.ContextJSON), &m)
	return m
}

func SetInstanceContext(inst *WorkflowInstance, ctx map[string]interface{}) error {
	b, err := json.Marshal(ctx)
	if err != nil {
		return err
	}
	inst.ContextJSON = string(b)
	return nil
}

func GetSendIDForInstanceNode(instanceID int64, nodeKey string) (int64, error) {
	var outputJSON string
	err := db.QueryRow(`
		SELECT output_json FROM workflow_executions
		WHERE instance_id = ? AND node_key = ? AND status = 'succeeded'
		ORDER BY id DESC LIMIT 1
	`, instanceID, nodeKey).Scan(&outputJSON)
	if err != nil {
		return 0, err
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal([]byte(outputJSON), &m); err != nil {
		return 0, fmt.Errorf("invalid execution output")
	}
	switch v := m["email_send_id"].(type) {
	case float64:
		return int64(v), nil
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	}
	return 0, fmt.Errorf("no email_send_id in execution")
}

func GetExecutionsForInstance(instanceID int64) ([]WorkflowExecution, error) {
	rows, err := db.Query(`
		SELECT id, instance_id, node_key, execution_key, status, started_at, finished_at, error_message, output_json
		FROM workflow_executions WHERE instance_id = ? ORDER BY id ASC
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []WorkflowExecution
	for rows.Next() {
		var e WorkflowExecution
		var fin sqlNullTime
		if err := rows.Scan(&e.ID, &e.InstanceID, &e.NodeKey, &e.ExecutionKey, &e.Status, &e.StartedAt, &fin, &e.ErrorMessage, &e.OutputJSON); err != nil {
			return nil, err
		}
		if fin.Valid {
			t := fin.Time
			e.FinishedAt = &t
		}
		list = append(list, e)
	}
	return list, nil
}

type WorkflowExecution struct {
	ID           int64
	InstanceID   int64
	NodeKey      string
	ExecutionKey string
	Status       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	ErrorMessage string
	OutputJSON   string
}

func CancelInstance(instanceID int64) error {
	_, err := db.Exec(`
		UPDATE workflow_instances SET status = 'cancelled', completed_at = CURRENT_TIMESTAMP,
			lock_token = NULL, lock_expires_at = NULL WHERE id = ? AND status IN ('active', 'waiting')
	`, instanceID)
	return err
}

func CountInstancesByStatus(workflowID int64) (map[string]int, error) {
	rows, err := db.Query(`
		SELECT wi.status, COUNT(*) FROM workflow_instances wi
		INNER JOIN workflow_versions wv ON wv.id = wi.workflow_version_id
		WHERE wv.workflow_id = ? GROUP BY wi.status
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if rows.Scan(&status, &n) == nil {
			m[status] = n
		}
	}
	return m, nil
}

func CountInstancesAtNode(versionID int64, nodeKey string) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM workflow_instances WHERE workflow_version_id = ? AND current_node_key = ? AND status IN ('active', 'waiting')
	`, versionID, nodeKey).Scan(&n)
	return n, err
}

func StartWorkflowForCampaign(campaignID, versionID int64, contactIDs []int64) (int, error) {
	entry, err := GetEntryNodeKey(versionID)
	if err != nil {
		return 0, fmt.Errorf("no entry node: %w", err)
	}
	started := 0
	for _, cid := range contactIDs {
		id, err := CreateWorkflowInstance(versionID, cid, campaignID, entry)
		if err != nil {
			continue
		}
		v, _ := GetWorkflowVersion(versionID)
		_, _ = InsertContactEvent(ContactEventInput{
			ContactID:          cid,
			CampaignID:         campaignID,
			WorkflowID:         v.WorkflowID,
			WorkflowInstanceID: id,
			EventType:          "WORKFLOW_STARTED",
			Metadata:           map[string]interface{}{"node_key": entry},
		})
		started++
	}
	return started, nil
}
