package workflow

import (
	"fmt"
	"time"

	"emailtracker.com/model"
)

var registry = map[string]NodeExecutor{}

func Register(e NodeExecutor) {
	registry[e.Type()] = e
}

func GetExecutor(nodeType string) (NodeExecutor, bool) {
	e, ok := registry[nodeType]
	return e, ok
}

func init() {
	Register(&TriggerExecutor{})
	Register(&SendEmailExecutor{})
	Register(&WaitExecutor{})
	Register(&EndExecutor{})
	Register(&ConditionExecutor{})
	Register(&TemperatureConditionExecutor{})
}

type TriggerExecutor struct{}

func (TriggerExecutor) Type() string { return "trigger_campaign_started" }

func (TriggerExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	return NodeResult{NextEdgeType: "default"}, nil
}

type SendEmailExecutor struct{}

func (SendEmailExecutor) Type() string { return "action_send_email" }

func (SendEmailExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	execKey := fmt.Sprintf("%d:%s:send", ctx.Instance.ID, ctx.Node.NodeKey)
	exists, _ := model.ExecutionExists(execKey)
	if exists {
		return NodeResult{NextEdgeType: "default", SkipDuplicate: true}, nil
	}

	variant := ""
	instCtx := model.GetInstanceContext(&ctx.Instance)
	if v, ok := instCtx["variant"].(string); ok {
		variant = v
	}

	campaignID := int64(0)
	if ctx.Instance.CampaignID != nil {
		campaignID = *ctx.Instance.CampaignID
	}
	if campaignID > 0 && model.CampaignIsStopped(campaignID) {
		return NodeResult{Failed: true, ErrorMessage: "campaign stopped"}, nil
	}
	if campaignID > 0 {
		if block, reason := model.ShouldBlockWorkflowSend(campaignID, ctx.Instance.ContactID); block {
			_ = model.CancelActiveInstancesForContactCampaign(ctx.Instance.ContactID, campaignID)
			return NodeResult{Failed: true, ErrorMessage: reason}, nil
		}
	}

	templateID, err := model.ResolveCampaignSendTemplate(campaignID, ctx.Node.NodeKey, variant, ctx.Instance.WorkflowVersionID)
	if err != nil || templateID == 0 {
		if err == nil {
			err = fmt.Errorf("missing template for node %s", ctx.Node.NodeKey)
		}
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}

	sendID, err := ctx.Mailer.SendWorkflowEmail(templateID, ctx.Instance.ContactID, campaignID, variant, ctx.Instance.ID)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}

	instCtx["last_send_id"] = sendID
	_ = model.SetInstanceContext(&ctx.Instance, instCtx)

	_, _ = model.CreateExecution(ctx.Instance.ID, ctx.Node.NodeKey, execKey, "succeeded",
		fmt.Sprintf(`{"email_send_id":%d}`, sendID), "")

	return NodeResult{
		NextEdgeType: "default",
		OutputJSON:   map[string]interface{}{"email_send_id": sendID},
	}, nil
}

type WaitExecutor struct{}

func (WaitExecutor) Type() string { return "action_wait" }

func (WaitExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	cfg := model.ParseNodeConfig(ctx.Node.ConfigJSON)
	secs := 0
	if v, ok := cfg["duration_seconds"].(float64); ok {
		secs = int(v)
	}
	if secs <= 0 {
		secs = 86400
	}
	wake := time.Now().Add(time.Duration(secs) * time.Second)
	return NodeResult{
		WakeAt:       &wake,
		WaitForEvent: "",
	}, nil
}

type EndExecutor struct{}

func (EndExecutor) Type() string { return "action_end" }

func (EndExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	now := time.Now()
	ctx.Instance.CompletedAt = &now
	_, _ = model.InsertContactEvent(model.ContactEventInput{
		ContactID:          ctx.Instance.ContactID,
		WorkflowInstanceID: ctx.Instance.ID,
		WorkflowID:         ctx.WorkflowID,
		EventType:          "WORKFLOW_COMPLETED",
	})
	return NodeResult{Complete: true}, nil
}

type ConditionExecutor struct{}

func (ConditionExecutor) Type() string { return "condition_engagement" }

func (ConditionExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	cfg := model.ParseNodeConfig(ctx.Node.ConfigJSON)
	predicate, _ := cfg["predicate"].(string)
	params, _ := cfg["params"].(map[string]interface{})
	if params == nil {
		params = map[string]interface{}{}
	}

	wakeAt, earlyEdge, err := NegativePredicateWait(predicate, params, ctx.Instance)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}
	if earlyEdge != "" {
		return NodeResult{NextEdgeType: earlyEdge}, nil
	}
	if wakeAt != nil {
		return NodeResult{WakeAt: wakeAt}, nil
	}

	ok, err := EvaluateCondition(predicate, params, ctx.Instance, ctx.Instance.ContactID)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}
	if ok {
		return NodeResult{NextEdgeType: "true"}, nil
	}
	return NodeResult{NextEdgeType: "false"}, nil
}

// TemperatureConditionExecutor branches on campaign lead temperature (hot/warm/cold).
type TemperatureConditionExecutor struct{}

func (TemperatureConditionExecutor) Type() string { return "condition_temperature" }

func (TemperatureConditionExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	campaignID := int64(0)
	if ctx.Instance.CampaignID != nil {
		campaignID = *ctx.Instance.CampaignID
	}
	if campaignID <= 0 {
		return NodeResult{NextEdgeType: model.LeadTemperatureCold}, nil
	}
	tier, err := model.ResolveLeadTemperature(campaignID, ctx.Instance.ContactID)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}
	switch tier {
	case model.LeadTemperatureHot, model.LeadTemperatureWarm, model.LeadTemperatureCold:
		return NodeResult{NextEdgeType: tier}, nil
	default:
		return NodeResult{NextEdgeType: model.LeadTemperatureCold}, nil
	}
}
