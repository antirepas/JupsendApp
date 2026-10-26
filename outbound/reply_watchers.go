package outbound

import (
	"context"
	"fmt"
	"log"
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/util"
	"emailtracker.com/workflow"
)

// MaybeEnqueueReplyWatcher starts attached reply-watcher workflows for a matching intent.
// The workflow graph owns wait → send; the mapped template is sent as written (no AI rewrite).
func MaybeEnqueueReplyWatcher(campaignID, contactID, messageID int64, intent string) {
	switch model.NormalizeReplyIntent(intent) {
	case "unsubscribe", "wrong_person", "":
		return
	}
	matches, err := model.FindReplyWatcherTriggersForCampaign(campaignID, intent)
	if err != nil {
		log.Printf("reply watcher lookup campaign=%d: %v", campaignID, err)
		return
	}
	ctxJSON := model.MarshalIntentContext(intent, messageID)
	started := 0
	for _, m := range matches {
		id, err := model.CreateReplyWatcherInstance(m.VersionID, contactID, campaignID, m.TriggerKey, ctxJSON)
		if err != nil {
			log.Printf("reply watcher instance campaign=%d wf=%d: %v", campaignID, m.WorkflowID, err)
			continue
		}
		started++
		_, _ = model.InsertContactEvent(model.ContactEventInput{
			ContactID:          contactID,
			CampaignID:         campaignID,
			WorkflowID:         m.WorkflowID,
			WorkflowInstanceID: id,
			EventType:          "REPLY_WATCHER_STARTED",
		})
	}
	if started > 0 {
		if eng := workflow.GetEngine(); eng != nil {
			go eng.ProcessDueInstances()
		}
	}
}

// ProcessDueReplyAutoJobs verifies and sends due auto-replies.
func ProcessDueReplyAutoJobs() {
	jobs, err := model.ClaimDueReplyAutoJobs(40)
	if err != nil {
		log.Printf("reply auto claim: %v", err)
		return
	}
	for _, job := range jobs {
		if err := processReplyAutoJob(job); err != nil {
			log.Printf("reply auto job %d: %v", job.ID, err)
		}
	}
}

func processReplyAutoJob(job model.ReplyAutoJob) error {
	// Mark processing to reduce double-runs (best-effort).
	_ = model.UpdateReplyAutoJob(job.ID, "processing", "", "", 0)

	camp, err := model.GetCampaign(job.CampaignID)
	if err != nil || camp.Status == "stopped" {
		return model.UpdateReplyAutoJob(job.ID, "cancelled", "", "campaign unavailable", 0)
	}
	if block, reason := model.ShouldBlockReplyWatcherSend(job.CampaignID, job.ContactID); block {
		return model.UpdateReplyAutoJob(job.ID, "cancelled", "", reason, 0)
	}

	_, _, contactID, userID, err := model.GetConversationMessageText(job.MessageID)
	if err != nil {
		return model.UpdateReplyAutoJob(job.ID, "failed", "", err.Error(), 0)
	}
	tmpl, err := model.GetTemplate(job.TemplateID)
	if err != nil {
		return model.UpdateReplyAutoJob(job.ID, "failed", "", "template missing", 0)
	}
	_, contactVars, err := model.GetContact(contactID)
	if err != nil {
		return model.UpdateReplyAutoJob(job.ID, "failed", "", "contact missing", 0)
	}

	renderedSubj, renderedBody, missing, err := util.RenderEmail(tmpl.Subject, tmpl.Body, contactVars, util.RenderOptions{
		UserID: userID,
		Ctx:    context.Background(),
	})
	if err != nil {
		return model.UpdateReplyAutoJob(job.ID, "failed", "", err.Error(), 0)
	}
	if len(missing) > 0 {
		note := fmt.Sprintf("[Auto-reply skipped] Missing variables: %s", strings.Join(missing, ", "))
		_ = model.InsertSystemConversationNote(userID, contactID, note)
		return model.UpdateReplyAutoJob(job.ID, "skipped", "missing_vars", note, 0)
	}
	sendID, err := SendManualReply(ManualReplyInput{
		UserID:           userID,
		ContactID:        contactID,
		CampaignID:       job.CampaignID,
		TemplateID:       job.TemplateID,
		Subject:          renderedSubj,
		BodyHTML:         renderedBody,
		ReplyToMessageID: job.MessageID,
	})
	if err != nil {
		note := "[Auto-reply failed] " + err.Error()
		_ = model.InsertSystemConversationNote(userID, contactID, note)
		return model.UpdateReplyAutoJob(job.ID, "failed", "", err.Error(), 0)
	}
	return model.UpdateReplyAutoJob(job.ID, "sent", "send", "", sendID)
}
