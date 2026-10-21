package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/ai"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

// CampaignJevReview runs a TypeSafe Jev review of the whole campaign
// (mode, A/B, follow-ups, audience coverage) — no per-contact PII.
func CampaignJevReview(ctx *gin.Context) {
	userID, ok := requireJEV(ctx)
	if !ok {
		return
	}
	// Campaign reviews are heavier than a single template — require 2 credits.
	if _, rem, okRem := model.AICreditsRemaining(userID); !okRem || rem < 2 {
		cap, remaining, _ := model.AICreditsRemaining(userID)
		ctx.JSON(http.StatusTooManyRequests, gin.H{
			"error":     "AI credits exhausted (campaign review needs 2)",
			"remaining": remaining,
			"cap":       cap,
		})
		return
	}

	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || campaignID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign id"})
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}

	in, err := buildCampaignReviewInput(userID, campaign)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(in.Emails) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "campaign has no email templates mapped yet"})
		return
	}

	result, err := ai.ReviewCampaign(ctx.Request.Context(), ai.NewJevClient(), in)
	if err != nil {
		respondAIError(ctx, err)
		return
	}
	if _, _, ok := model.ConsumeAICredits(userID, 2); !ok {
		// Review already ran; still report remaining honestly.
	}
	_, rem, _ := model.AICreditsRemaining(userID)
	ctx.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"review":    result,
		"remaining": rem,
		"credits":   2,
	})
}

func buildCampaignReviewInput(userID int64, c model.Campaign) (ai.CampaignReviewInput, error) {
	mode := strings.TrimSpace(c.ExecutionMode)
	if mode == "" {
		mode = "bulk"
	}
	if mode == "bulk" && c.TemplateBID > 0 {
		mode = "bulk_ab"
	}

	contactCount := 0
	status := c.Status
	if item, err := model.GetCampaignListItemByID(c.ID, userID); err == nil {
		contactCount = item.ContactCount
		if item.DisplayStatus != "" {
			status = item.DisplayStatus
		}
	}

	emails := collectCampaignReviewEmails(userID, c, mode)
	mergedIDs := []int64{c.TemplateAID, c.TemplateBID}
	if mode == "sequence" || mode == "sequence_ab" {
		if steps, err := model.ListCampaignSequenceSteps(c.ID); err == nil {
			for _, s := range steps {
				if s.TemplateID > 0 {
					mergedIDs = append(mergedIDs, s.TemplateID)
				}
			}
		}
	}
	if mode == "workflow" || mode == "workflow_ab" {
		if mappings, err := model.GetCampaignWorkflowTemplates(c.ID); err == nil {
			for _, tid := range mappings {
				mergedIDs = append(mergedIDs, tid)
			}
		}
	}
	mergedVars, _ := model.MergeTemplateVariables(userID, mergedIDs)

	var missing int
	if mode == "workflow" || mode == "workflow_ab" {
		missing, _ = model.CountCampaignContactsMissingAnyVars(c.ID, mergedVars)
	} else {
		_, aVars, _ := model.GetTemplateByID(c.TemplateAID, userID)
		var bVars []string
		hasB := c.TemplateBID > 0
		if hasB {
			_, bVars, _ = model.GetTemplateByID(c.TemplateBID, userID)
		}
		missing, _ = model.CountCampaignContactsMissingVars(c.ID, aVars, bVars, hasB)
	}
	ready := contactCount - missing
	if ready < 0 {
		ready = 0
	}

	coverageRaw, _ := model.CampaignVariableCoverage(c.ID, mergedVars)
	coverage := make([]ai.CampaignVarStat, 0, len(coverageRaw))
	for _, v := range coverageRaw {
		coverage = append(coverage, ai.CampaignVarStat{
			Key: v.Key, Filled: v.Filled, Total: v.Total, Missing: v.Missing, Pct: v.Pct,
		})
	}

	mailboxCount := 0
	if sel, err := model.GetCampaignSMTPSelection(userID, c.ID); err == nil {
		mailboxCount = len(sel.SelectedIDs)
		if mailboxCount == 0 {
			mailboxCount = len(sel.Accounts)
		}
	}

	workflowName := ""
	if (mode == "workflow" || mode == "workflow_ab") && c.WorkflowVersionID > 0 {
		if info, err := model.GetWorkflowForVersion(c.WorkflowVersionID); err == nil {
			workflowName = info.WorkflowName
		}
	}

	return ai.CampaignReviewInput{
		Name:                 c.Name,
		Mode:                 mode,
		Status:               status,
		ContactCount:         contactCount,
		ReadyContactCount:    ready,
		MissingVarsContacts:  missing,
		ExperimentVariable:   c.ExperimentVariable,
		ExperimentHypothesis: c.ExperimentHypothesis,
		OpenTracking:         c.OpenTrackingEnabled,
		StopOnReply:          c.StopOnReply,
		StopOnHot:            c.StopOnHot,
		MailboxCount:         mailboxCount,
		WorkflowName:         workflowName,
		Emails:               emails,
		VarCoverage:          coverage,
		MergedVars:           mergedVars,
	}, nil
}

func collectCampaignReviewEmails(userID int64, c model.Campaign, mode string) []ai.CampaignEmailStep {
	var out []ai.CampaignEmailStep
	appendTpl := func(role, label, nodeKey string, tid int64) {
		if tid <= 0 {
			return
		}
		t, vars, err := model.GetTemplateByID(tid, userID)
		if err != nil {
			return
		}
		bodyPlain := util.StripHTML(util.NormalizeQuillHTML(t.Body))
		out = append(out, ai.CampaignEmailStep{
			Role:         role,
			Label:        label,
			NodeKey:      nodeKey,
			TemplateName: t.Name,
			Subject:      t.Subject,
			BodyPlain:    bodyPlain,
			Variables:    vars,
		})
	}

	switch mode {
	case "sequence", "sequence_ab":
		steps, err := model.ListCampaignSequenceSteps(c.ID)
		if err != nil || len(steps) == 0 {
			appendTpl("first_a", "Variant A", "", c.TemplateAID)
			appendTpl("first_b", "Variant B", "", c.TemplateBID)
			return out
		}
		for i, step := range steps {
			if i == 0 {
				appendTpl("first_a", "Email 1 (A)", "", c.TemplateAID)
				if mode == "sequence_ab" || c.TemplateBID > 0 {
					appendTpl("first_b", "Email 1 (B)", "", c.TemplateBID)
				}
				continue
			}
			appendTpl("follow_up", fmt.Sprintf("Follow-up %d", i+1), "", step.TemplateID)
		}
	case "workflow", "workflow_ab":
		steps, err := model.GetCampaignWorkflowStepDisplayForCampaign(c, userID)
		if err != nil {
			appendTpl("first_a", "Variant A", "", c.TemplateAID)
			appendTpl("first_b", "Variant B", "", c.TemplateBID)
			return out
		}
		firstSend := true
		for _, step := range steps {
			if step.NodeType != "action_send_email" {
				continue
			}
			if step.IsHybridAB || (mode == "workflow_ab" && firstSend) {
				appendTpl("first_a", step.Label+" (A)", step.NodeKey, c.TemplateAID)
				appendTpl("first_b", step.Label+" (B)", step.NodeKey, c.TemplateBID)
				firstSend = false
				continue
			}
			role := "follow_up"
			label := step.Label
			if firstSend {
				role = "first_a"
				firstSend = false
			} else {
				label = "Follow-up: " + step.Label
			}
			tid := step.TemplateID
			if tid <= 0 && role == "first_a" {
				tid = c.TemplateAID
			}
			appendTpl(role, label, step.NodeKey, tid)
		}
	default:
		appendTpl("first_a", "Variant A", "", c.TemplateAID)
		if c.TemplateBID > 0 {
			appendTpl("first_b", "Variant B", "", c.TemplateBID)
		}
	}
	return out
}
