package model

import (
	"encoding/json"
	"fmt"
	"time"

	"emailtracker.com/db"
)

type Workflow struct {
	ID               int64
	Name             string
	Description      string
	CurrentVersionID int64
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type WorkflowVersion struct {
	ID          int64
	WorkflowID  int64
	Version     int
	Status      string
	PublishedAt *time.Time
	CreatedAt   time.Time
}

type WorkflowNode struct {
	ID         int64
	VersionID  int64
	NodeKey    string
	NodeType   string
	Label      string
	ConfigJSON string
	PositionX  float64
	PositionY  float64
}

type WorkflowEdge struct {
	ID            int64
	VersionID     int64
	SourceNodeKey string
	TargetNodeKey string
	EdgeType      string
	Priority      int
	ConditionJSON string
}

type WorkflowGraph struct {
	Version WorkflowVersion
	Nodes   []WorkflowNode
	Edges   []WorkflowEdge
}

func ListWorkflows(userID int64) ([]Workflow, error) {
	rows, err := db.Query(`
		SELECT id, name, description, COALESCE(current_version_id, 0), status, created_at, updated_at
		FROM workflows WHERE tenant_id = ? ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Workflow
	for rows.Next() {
		var w Workflow
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.CurrentVersionID, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, nil
}

func GetPublishedWorkflows(userID int64) ([]Workflow, error) {
	rows, err := db.Query(`
		SELECT w.id, w.name, w.description, COALESCE(w.current_version_id, 0), w.status, w.created_at, w.updated_at
		FROM workflows w
		WHERE w.tenant_id = ? AND w.current_version_id IS NOT NULL AND w.status = 'active'
		ORDER BY w.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Workflow
	for rows.Next() {
		var w Workflow
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.CurrentVersionID, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, nil
}

func CreateWorkflow(userID int64, name, description string) (int64, error) {
	row := db.QueryRow(`
		INSERT INTO workflows (name, description, tenant_id) VALUES (?, ?, ?) RETURNING id
	`, name, description, userID)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		return 0, err
	}
	vid, err := CreateWorkflowVersion(id)
	if err != nil {
		return id, err
	}
	_, _ = db.Exec(`UPDATE workflows SET current_version_id = ? WHERE id = ?`, vid, id)
	return id, nil
}

func GetWorkflow(id int64) (Workflow, error) {
	row := db.QueryRow(`
		SELECT id, name, description, COALESCE(current_version_id, 0), status, created_at, updated_at
		FROM workflows WHERE id = ?
	`, id)
	var w Workflow
	err := row.Scan(&w.ID, &w.Name, &w.Description, &w.CurrentVersionID, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func GetWorkflowForUser(id, userID int64) (Workflow, error) {
	row := db.QueryRow(`
		SELECT id, name, description, COALESCE(current_version_id, 0), status, created_at, updated_at
		FROM workflows WHERE id = ? AND tenant_id = ?
	`, id, userID)
	var w Workflow
	err := row.Scan(&w.ID, &w.Name, &w.Description, &w.CurrentVersionID, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func CreateWorkflowVersion(workflowID int64) (int64, error) {
	var maxVer int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM workflow_versions WHERE workflow_id = ?`, workflowID).Scan(&maxVer)
	row := db.QueryRow(`
		INSERT INTO workflow_versions (workflow_id, version, status) VALUES (?, ?, 'draft') RETURNING id
	`, workflowID, maxVer+1)
	var id int64
	err := row.Scan(&id)
	return id, err
}

func GetWorkflowVersion(versionID int64) (WorkflowVersion, error) {
	row := db.QueryRow(`
		SELECT id, workflow_id, version, status, published_at, created_at FROM workflow_versions WHERE id = ?
	`, versionID)
	var v WorkflowVersion
	var pub sqlNullTime
	err := row.Scan(&v.ID, &v.WorkflowID, &v.Version, &v.Status, &pub, &v.CreatedAt)
	if pub.Valid {
		t := pub.Time
		v.PublishedAt = &t
	}
	return v, err
}

func GetWorkflowGraph(versionID int64) (WorkflowGraph, error) {
	v, err := GetWorkflowVersion(versionID)
	if err != nil {
		return WorkflowGraph{}, err
	}
	nodes, err := getWorkflowNodes(versionID)
	if err != nil {
		return WorkflowGraph{}, err
	}
	edges, err := getWorkflowEdges(versionID)
	if err != nil {
		return WorkflowGraph{}, err
	}
	return WorkflowGraph{Version: v, Nodes: nodes, Edges: edges}, nil
}

func getWorkflowNodes(versionID int64) ([]WorkflowNode, error) {
	rows, err := db.Query(`
		SELECT id, version_id, node_key, node_type, label, config_json, position_x, position_y
		FROM workflow_nodes WHERE version_id = ?
	`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []WorkflowNode
	for rows.Next() {
		var n WorkflowNode
		if err := rows.Scan(&n.ID, &n.VersionID, &n.NodeKey, &n.NodeType, &n.Label, &n.ConfigJSON, &n.PositionX, &n.PositionY); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func getWorkflowEdges(versionID int64) ([]WorkflowEdge, error) {
	rows, err := db.Query(`
		SELECT id, version_id, source_node_key, target_node_key, edge_type, priority, condition_json
		FROM workflow_edges WHERE version_id = ?
	`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []WorkflowEdge
	for rows.Next() {
		var e WorkflowEdge
		if err := rows.Scan(&e.ID, &e.VersionID, &e.SourceNodeKey, &e.TargetNodeKey, &e.EdgeType, &e.Priority, &e.ConditionJSON); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, nil
}

type GraphSaveInput struct {
	Nodes []WorkflowNodeInput
	Edges []WorkflowEdgeInput
}

type WorkflowNodeInput struct {
	NodeKey    string  `json:"node_key"`
	NodeType   string  `json:"node_type"`
	Label      string  `json:"label"`
	ConfigJSON string  `json:"config_json"`
	PositionX  float64 `json:"position_x"`
	PositionY  float64 `json:"position_y"`
}

type WorkflowEdgeInput struct {
	SourceNodeKey string `json:"source_node_key"`
	TargetNodeKey string `json:"target_node_key"`
	EdgeType      string `json:"edge_type"`
	Priority      int    `json:"priority"`
	ConditionJSON string `json:"condition_json"`
}

func SaveWorkflowGraph(versionID int64, input GraphSaveInput) error {
	v, err := GetWorkflowVersion(versionID)
	if err != nil {
		return err
	}
	if v.Status != "draft" {
		return fmt.Errorf("only draft versions can be edited")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM workflow_edges WHERE version_id = ?`, versionID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM workflow_nodes WHERE version_id = ?`, versionID)
	if err != nil {
		return err
	}

	for _, n := range input.Nodes {
		cfg := n.ConfigJSON
		if cfg == "" {
			cfg = "{}"
		}
		_, err = tx.Exec(`
			INSERT INTO workflow_nodes (version_id, node_key, node_type, label, config_json, position_x, position_y)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, versionID, n.NodeKey, n.NodeType, n.Label, cfg, n.PositionX, n.PositionY)
		if err != nil {
			return err
		}
	}

	for _, e := range input.Edges {
		cond := e.ConditionJSON
		if cond == "" {
			cond = "{}"
		}
		edgeType := e.EdgeType
		if edgeType == "" {
			edgeType = "default"
		}
		_, err = tx.Exec(`
			INSERT INTO workflow_edges (version_id, source_node_key, target_node_key, edge_type, priority, condition_json)
			VALUES (?, ?, ?, ?, ?, ?)
		`, versionID, e.SourceNodeKey, e.TargetNodeKey, edgeType, e.Priority, cond)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(`UPDATE workflows SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, v.WorkflowID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func PublishWorkflowVersion(workflowID, versionID int64) error {
	v, err := GetWorkflowVersion(versionID)
	if err != nil {
		return err
	}
	if v.WorkflowID != workflowID {
		return fmt.Errorf("version does not belong to workflow")
	}
	if errs := ValidateWorkflowGraph(versionID); len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", errs[0])
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE workflow_versions SET status = 'archived' WHERE workflow_id = ? AND status = 'published'`, workflowID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE workflow_versions SET status = 'published', published_at = CURRENT_TIMESTAMP WHERE id = ?`, versionID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE workflows SET current_version_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, versionID, workflowID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func ValidateWorkflowGraph(versionID int64) []string {
	g, err := GetWorkflowGraph(versionID)
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	if len(g.Nodes) == 0 {
		return []string{"workflow has no nodes"}
	}

	entryCount := 0
	for _, n := range g.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entryCount++
		}
	}
	if entryCount != 1 {
		errs = append(errs, "workflow must have exactly one trigger_campaign_started node")
	}

	nodeKeys := make(map[string]bool)
	nodeTypes := make(map[string]string)
	for _, n := range g.Nodes {
		nodeKeys[n.NodeKey] = true
		nodeTypes[n.NodeKey] = n.NodeType
	}

	adj := make(map[string][]WorkflowEdge)
	for _, e := range g.Edges {
		if !nodeKeys[e.SourceNodeKey] || !nodeKeys[e.TargetNodeKey] {
			errs = append(errs, fmt.Sprintf("edge references missing node: %s -> %s", e.SourceNodeKey, e.TargetNodeKey))
		}
		adj[e.SourceNodeKey] = append(adj[e.SourceNodeKey], e)
	}

	var entryKey string
	for _, n := range g.Nodes {
		if n.NodeType == "trigger_campaign_started" {
			entryKey = n.NodeKey
		}
	}

	reachable := map[string]bool{}
	queue := []string{entryKey}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		if reachable[k] {
			continue
		}
		reachable[k] = true
		for _, e := range adj[k] {
			queue = append(queue, e.TargetNodeKey)
		}
	}

	for k := range nodeKeys {
		if !reachable[k] {
			errs = append(errs, fmt.Sprintf("node %s is not reachable from entry", k))
		}
	}

	hasEnd := false
	for _, n := range g.Nodes {
		if n.NodeType == "action_end" {
			hasEnd = true
		}
		if n.NodeType == "condition_engagement" {
			hasTrue, hasFalse := false, false
			for _, e := range adj[n.NodeKey] {
				if e.EdgeType == "true" {
					hasTrue = true
				}
				if e.EdgeType == "false" {
					hasFalse = true
				}
			}
			if !hasTrue || !hasFalse {
				errs = append(errs, fmt.Sprintf("condition node %s needs true and false edges", n.NodeKey))
			}
			if msg := validateConditionEmailRef(n, nodeTypes); msg != "" {
				errs = append(errs, msg)
			}
		}
	}
	if !hasEnd {
		errs = append(errs, "workflow must include at least one action_end node")
	}

	return errs
}

func GetEntryNodeKey(versionID int64) (string, error) {
	var key string
	err := db.QueryRow(`
		SELECT node_key FROM workflow_nodes WHERE version_id = ? AND node_type = 'trigger_campaign_started' LIMIT 1
	`, versionID).Scan(&key)
	return key, err
}

func ParseNodeConfig(configJSON string) map[string]interface{} {
	m := map[string]interface{}{}
	_ = json.Unmarshal([]byte(configJSON), &m)
	return m
}

type sqlNullTime struct {
	Time  time.Time
	Valid bool
}

func (n *sqlNullTime) Scan(value interface{}) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		n.Time = v
		n.Valid = true
	case string:
		t, err := time.Parse("2006-01-02 15:04:05-07:00", v)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", v)
		}
		if err != nil {
			return err
		}
		n.Time = t
		n.Valid = true
	}
	return nil
}
