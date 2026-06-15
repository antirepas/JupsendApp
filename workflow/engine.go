package workflow

import (
	"fmt"
	"log"
	"time"

	"emailtracker.com/model"
)

const maxStepsPerRun = 15

type Engine struct {
	Mailer MailSender
}

func NewEngine(mailer MailSender) *Engine {
	return &Engine{Mailer: mailer}
}

func (e *Engine) ProcessInstance(instanceID int64) error {
	inst, err := model.GetWorkflowInstance(instanceID)
	if err != nil {
		return err
	}
	if inst.Status == "completed" || inst.Status == "cancelled" || inst.Status == "failed" {
		model.ReleaseInstanceLock(instanceID)
		return nil
	}

	if inst.Status == "waiting" && inst.NextWakeAt != nil && inst.NextWakeAt.After(time.Now()) {
		model.ReleaseInstanceLock(instanceID)
		return nil
	}

	graph, err := model.GetWorkflowGraph(inst.WorkflowVersionID)
	if err != nil {
		return err
	}

	version, _ := model.GetWorkflowVersion(inst.WorkflowVersionID)
	workflow, _ := model.GetWorkflow(version.WorkflowID)

	nodeMap := map[string]model.WorkflowNode{}
	for _, n := range graph.Nodes {
		nodeMap[n.NodeKey] = n
	}

	adj := buildAdjacency(graph.Edges)

	for step := 0; step < maxStepsPerRun; step++ {
		node, ok := nodeMap[inst.CurrentNodeKey]
		if !ok {
			inst.Status = "failed"
			_ = model.UpdateInstanceState(inst)
			return fmt.Errorf("unknown node %s", inst.CurrentNodeKey)
		}

		executor, ok := GetExecutor(node.NodeType)
		if !ok {
			inst.Status = "failed"
			_ = model.UpdateInstanceState(inst)
			return fmt.Errorf("no executor for %s", node.NodeType)
		}

		ctx := ExecutionContext{
			Instance:   inst,
			Node:       node,
			Graph:      graph,
			Mailer:     e.Mailer,
			WorkflowID: workflow.ID,
		}

		result, err := executor.Execute(ctx)
		if err != nil {
			inst.Status = "failed"
			_ = model.UpdateInstanceState(inst)
			return err
		}

		if result.Failed {
			inst.Status = "failed"
			_ = model.UpdateInstanceState(inst)
			return fmt.Errorf("%s", result.ErrorMessage)
		}

		if result.Complete {
			inst.Status = "completed"
			now := time.Now()
			inst.CompletedAt = &now
			inst.WaitingForEvent = ""
			inst.NextWakeAt = nil
			_ = model.UpdateInstanceState(inst)
			return nil
		}

		if result.WakeAt != nil {
			inst.Status = "waiting"
			inst.NextWakeAt = result.WakeAt
			inst.WaitingForEvent = result.WaitForEvent
			_ = model.UpdateInstanceState(inst)
			return nil
		}

		edgeType := result.NextEdgeType
		if edgeType == "" {
			edgeType = "default"
		}
		nextKey, ok := pickNextNode(adj, inst.CurrentNodeKey, edgeType)
		if !ok {
			inst.Status = "failed"
			_ = model.UpdateInstanceState(inst)
			return fmt.Errorf("no edge from %s type %s", inst.CurrentNodeKey, edgeType)
		}

		inst.CurrentNodeKey = nextKey
		inst.Status = "active"
		inst.NextWakeAt = nil
		inst.WaitingForEvent = ""
		_ = model.UpdateInstanceState(inst)

		nextNode := nodeMap[nextKey]
		if nextNode.NodeType == "action_wait" || nextNode.NodeType == "action_send_email" || nextNode.NodeType == "condition_engagement" || nextNode.NodeType == "trigger_campaign_started" {
			continue
		}
		if nextNode.NodeType == "action_end" {
			continue
		}
	}

	_ = model.UpdateInstanceState(inst)
	return nil
}

func buildAdjacency(edges []model.WorkflowEdge) map[string][]model.WorkflowEdge {
	adj := map[string][]model.WorkflowEdge{}
	for _, e := range edges {
		adj[e.SourceNodeKey] = append(adj[e.SourceNodeKey], e)
	}
	return adj
}

func pickNextNode(adj map[string][]model.WorkflowEdge, from, edgeType string) (string, bool) {
	edges := adj[from]
	for _, e := range edges {
		if e.EdgeType == edgeType {
			return e.TargetNodeKey, true
		}
	}
	for _, e := range edges {
		if e.EdgeType == "default" {
			return e.TargetNodeKey, true
		}
	}
	return "", false
}

func (e *Engine) ProcessDueInstances() {
	ids, err := model.ClaimDueInstances(50)
	if err != nil {
		log.Printf("workflow: claim error: %v", err)
		return
	}
	for _, id := range ids {
		if err := e.ProcessInstance(id); err != nil {
			log.Printf("workflow: instance %d: %v", id, err)
		}
	}
}

func (e *Engine) ProcessWokenInstances(ids []int64) {
	for _, id := range ids {
		ok, _ := model.ClaimInstance(id)
		if !ok {
			continue
		}
		if err := e.ProcessInstance(id); err != nil {
			log.Printf("workflow wake %d: %v", id, err)
		}
	}
}
