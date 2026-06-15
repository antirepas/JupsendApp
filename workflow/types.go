package workflow

import (
	"time"

	"emailtracker.com/model"
)

type MailSender interface {
	SendWorkflowEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error)
}

type NodeResult struct {
	OutputJSON    map[string]interface{}
	NextEdgeType  string
	WakeAt        *time.Time
	WaitForEvent  string
	Complete      bool
	Failed        bool
	ErrorMessage  string
	SkipDuplicate bool
}

type ExecutionContext struct {
	Instance  model.WorkflowInstance
	Node      model.WorkflowNode
	Graph     model.WorkflowGraph
	Mailer    MailSender
	WorkflowID int64
}

type NodeExecutor interface {
	Type() string
	Execute(ctx ExecutionContext) (NodeResult, error)
}
