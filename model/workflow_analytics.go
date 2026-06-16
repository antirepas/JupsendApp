package model

import (
	"emailtracker.com/db"
)

type WorkflowNodeStat struct {
	NodeKey  string
	Label    string
	NodeType string
	Active   int
	Sends    int
	Opens    int
	Clicks   int
}

func GetWorkflowNodeStats(versionID int64) ([]WorkflowNodeStat, error) {
	graph, err := GetWorkflowGraph(versionID)
	if err != nil {
		return nil, err
	}
	var stats []WorkflowNodeStat
	for _, n := range graph.Nodes {
		active, _ := CountInstancesAtNode(versionID, n.NodeKey)
		s := WorkflowNodeStat{
			NodeKey:  n.NodeKey,
			Label:    n.Label,
			NodeType: n.NodeType,
			Active:   active,
		}
		if n.NodeType == "action_send_email" {
			var sends int
			_ = db.QueryRow(`
				SELECT COUNT(*) FROM workflow_executions we
				INNER JOIN workflow_instances wi ON wi.id = we.instance_id
				WHERE wi.workflow_version_id = ? AND we.node_key = ? AND we.status = 'succeeded'
			`, versionID, n.NodeKey).Scan(&sends)
			s.Sends = sends
		}
		stats = append(stats, s)
	}
	return stats, nil
}
