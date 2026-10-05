package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/util"
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
	claimed, err := model.TryBeginExecution(ctx.Instance.ID, ctx.Node.NodeKey, execKey)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}
	if !claimed {
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
		model.AbortStartedExecution(execKey)
		return NodeResult{Failed: true, ErrorMessage: "campaign stopped"}, nil
	}
	if campaignID > 0 {
		if block, reason := model.ShouldBlockWorkflowSend(campaignID, ctx.Instance.ContactID); block {
			model.AbortStartedExecution(execKey)
			_ = model.CancelActiveInstancesForContactCampaign(ctx.Instance.ContactID, campaignID)
			return NodeResult{Failed: true, ErrorMessage: reason}, nil
		}
	}

	templateID, err := model.ResolveCampaignSendTemplate(campaignID, ctx.Node.NodeKey, variant, ctx.Instance.WorkflowVersionID)
	if err != nil || templateID == 0 {
		model.AbortStartedExecution(execKey)
		if err == nil {
			err = fmt.Errorf("missing template for node %s", ctx.Node.NodeKey)
		}
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}

	cfg := model.ParseNodeConfig(ctx.Node.ConfigJSON)
	openTrack := boolCfg(cfg, "open_tracking", false)
	clickTrack := boolCfg(cfg, "click_tracking", false)

	tmpl, err := model.GetTemplate(templateID)
	if err != nil {
		model.AbortStartedExecution(execKey)
		return NodeResult{Failed: true, ErrorMessage: "template not found"}, nil
	}
	_, contactVars, err := model.GetContact(ctx.Instance.ContactID)
	if err != nil {
		model.AbortStartedExecution(execKey)
		return NodeResult{Failed: true, ErrorMessage: "contact not found"}, nil
	}
	if missing := util.MissingContactVarsForTemplates(contactVars, tmpl.Subject, tmpl.Body); len(missing) > 0 {
		model.AbortStartedExecution(execKey)
		return NodeResult{
			Failed:       true,
			ErrorMessage: "missing template variables: " + strings.Join(missing, ", "),
		}, nil
	}

	sendID, err := ctx.Mailer.SendWorkflowEmail(templateID, ctx.Instance.ContactID, campaignID, variant, ctx.Instance.ID, openTrack, clickTrack)
	if err != nil {
		model.AbortStartedExecution(execKey)
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}

	instCtx["last_send_id"] = sendID
	_ = model.SetInstanceContext(&ctx.Instance, instCtx)

	_ = model.CompleteExecution(execKey, fmt.Sprintf(`{"email_send_id":%d}`, sendID))

	return NodeResult{
		NextEdgeType: "default",
		OutputJSON:   map[string]interface{}{"email_send_id": sendID},
	}, nil
}

type WaitExecutor struct{}

func (WaitExecutor) Type() string { return "action_wait" }

func (WaitExecutor) Execute(ctx ExecutionContext) (NodeResult, error) {
	// Resume after a due wake: do not schedule another full wait (that used to loop forever).
	// Never advance while next_wake_at is still in the future.
	if ctx.Instance.Status == "waiting" {
		if ctx.Instance.NextWakeAt != nil && ctx.Instance.NextWakeAt.After(time.Now()) {
			wake := *ctx.Instance.NextWakeAt
			return NodeResult{WakeAt: &wake}, nil
		}
		execKey := fmt.Sprintf("%d:%s:wait-done", ctx.Instance.ID, ctx.Node.NodeKey)
		_, _ = model.CreateExecution(ctx.Instance.ID, ctx.Node.NodeKey, execKey, "succeeded", `{"wait":"completed"}`, "")
		return NodeResult{NextEdgeType: "default"}, nil
	}

	cfg := model.ParseNodeConfig(ctx.Node.ConfigJSON)
	secs := configDurationSeconds(cfg)
	if secs <= 0 {
		secs = 86400
	}
	wake := time.Now().Add(time.Duration(secs) * time.Second)
	return NodeResult{
		WakeAt:       &wake,
		WaitForEvent: "",
	}, nil
}

// configDurationSeconds reads wait length from node config (seconds, or days/hours helpers).
func configDurationSeconds(cfg map[string]interface{}) int {
	if cfg == nil {
		return 0
	}
	if n := intFromConfig(cfg["duration_seconds"]); n > 0 {
		return n
	}
	if n := intFromConfig(cfg["duration_hours"]); n > 0 {
		return n * 3600
	}
	if n := intFromConfig(cfg["duration_days"]); n > 0 {
		return n * 86400
	}
	return 0
}

func intFromConfig(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func boolCfg(cfg map[string]interface{}, key string, def bool) bool {
	if cfg == nil {
		return def
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return def
	}
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
		recordConditionExecution(ctx, earlyEdge)
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
		recordConditionExecution(ctx, "true")
		return NodeResult{NextEdgeType: "true"}, nil
	}
	recordConditionExecution(ctx, "false")
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
		recordConditionExecution(ctx, model.LeadTemperatureCold)
		return NodeResult{NextEdgeType: model.LeadTemperatureCold}, nil
	}
	tier, err := model.ResolveLeadTemperature(campaignID, ctx.Instance.ContactID)
	if err != nil {
		return NodeResult{Failed: true, ErrorMessage: err.Error()}, nil
	}
	switch tier {
	case model.LeadTemperatureHot, model.LeadTemperatureWarm, model.LeadTemperatureCold:
		recordConditionExecution(ctx, tier)
		return NodeResult{NextEdgeType: tier}, nil
	default:
		recordConditionExecution(ctx, model.LeadTemperatureCold)
		return NodeResult{NextEdgeType: model.LeadTemperatureCold}, nil
	}
}

func recordConditionExecution(ctx ExecutionContext, edgeType string) {
	execKey := fmt.Sprintf("%d:%s:%s", ctx.Instance.ID, ctx.Node.NodeKey, edgeType)
	_, _ = model.CreateExecution(ctx.Instance.ID, ctx.Node.NodeKey, execKey, "succeeded",
		fmt.Sprintf(`{"next_edge":%q}`, edgeType), "")
}
