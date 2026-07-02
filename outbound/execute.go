package outbound

import (
	"fmt"
	"log"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

func executeJob(job model.SendJob, account model.SMTPAccount) error {
	template, err := model.GetTemplate(job.TemplateID)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	contact, contactVars, err := model.GetContact(job.ContactID)
	if err != nil {
		return fmt.Errorf("contact: %w", err)
	}

	emailSendID := job.EmailSendID
	if emailSendID == 0 {
		return fmt.Errorf("missing email_send_id on job")
	}

	detail, err := model.GetEmailSendDetail(emailSendID)
	if err != nil {
		return fmt.Errorf("email send: %w", err)
	}
	trackID := detail.TrackingID
	baseURL := model.UserBaseURL(job.UserID)

	newBody, _ := util.RenderTemplate(template.Body, contactVars, "")
	newBody = util.WrapHTMLBody(newBody)
	newBody = util.InjectTrackingPixelWithBase(newBody, trackID, baseURL)
	newSubject, _ := util.RenderTemplate(template.Subject, contactVars, "")
	replacedLinksBody := util.RewriteLinksWithBase(newBody, emailSendID, baseURL)
	plainBody := util.StripHTML(replacedLinksBody)

	if model.UserIncludeUnsubscribeLink(job.UserID) {
		unsubURL := model.UnsubscribeURL(baseURL, job.UserID, job.ContactID)
		replacedLinksBody, plainBody = util.InjectUnsubscribeFooter(replacedLinksBody, plainBody, unsubURL)
	}

	from := account.SenderEmail()
	if from == "" {
		return fmt.Errorf("smtp account %d has no sender email configured", account.ID)
	}
	sender := util.NewEmailSender(account.SMTPHost, account.SMTPPort, account.SMTPUser, account.SMTPPassword, from)
	messageID := fmt.Sprintf("<%s@emailtracker>", trackID)
	meta := util.SendMeta{
		MessageID:          messageID,
		EmailTrackerSendID: fmt.Sprintf("%d", emailSendID),
		FromName:           account.FromName,
	}
	if account.IsGoogleOAuth() {
		token, err := model.GmailAccessToken(account)
		if err != nil {
			return fmt.Errorf("gmail oauth: %w", err)
		}
		err = sender.SendWithMetaOAuth(contact.Email, newSubject, plainBody, replacedLinksBody, meta, token)
	} else {
		err = sender.SendWithMeta(contact.Email, newSubject, plainBody, replacedLinksBody, meta)
	}
	if err != nil {
		return err
	}
	log.Printf("outbound: email_send=%d delivered to %s from %s", emailSendID, contact.Email, from)

	if err := model.MarkEmailSendSent(emailSendID, account.ID, job.ID); err != nil {
		return err
	}
	if err := model.CompleteSendJob(job.ID, account.ID); err != nil {
		return err
	}
	MarkAccountSent(account.ID)

	recordSendContactEvent(job.ContactID, job.CampaignID, job.WorkflowInstanceID, emailSendID, job.TemplateID)
	onJobCompleted(job, emailSendID)

	return nil
}

func recordSendContactEvent(contactID, campaignID, workflowInstanceID, emailSendID, templateID int64) {
	var wfID int64
	if workflowInstanceID > 0 {
		inst, err := model.GetWorkflowInstance(workflowInstanceID)
		if err == nil {
			v, _ := model.GetWorkflowVersion(inst.WorkflowVersionID)
			wfID = v.WorkflowID
		}
	}
	_, _ = model.InsertContactEvent(model.ContactEventInput{
		ContactID:          contactID,
		CampaignID:         campaignID,
		WorkflowID:         wfID,
		WorkflowInstanceID: workflowInstanceID,
		EmailSendID:        emailSendID,
		EventType:          "SEND",
		Metadata: map[string]interface{}{
			"template_id": templateID,
		},
	})
}

func onJobCompleted(job model.SendJob, emailSendID int64) {
	if job.WorkflowInstanceID > 0 {
		inst, err := model.GetWorkflowInstance(job.WorkflowInstanceID)
		if err != nil {
			return
		}
		ctxMap := model.GetInstanceContext(&inst)
		ctxMap["last_send_id"] = emailSendID
		_ = model.SetInstanceContext(&inst, ctxMap)
	}
	if job.CampaignID > 0 {
		reconcileCampaign(job.CampaignID)
	}
}

func reconcileCampaign(campaignID int64) {
	active, err := model.HasActiveCampaignJobs(campaignID)
	if err != nil || active {
		return
	}
	campaign, err := model.GetCampaign(campaignID)
	if err != nil || !campaign.IsSending {
		return
	}
	if err := model.MarkCampaignSent(campaignID); err != nil {
		log.Printf("campaign reconcile: %v", err)
	}
}

func handleJobFailure(job model.SendJob, sendErr error) {
	emailSendID := job.EmailSendID
	class := ClassifySMTPError(sendErr)
	attempts := job.Attempts + 1

	if class == ErrorPermanent || attempts >= job.MaxAttempts {
		status := "dead"
		if class == ErrorPermanent {
			status = "failed"
		}
		if attempts >= job.MaxAttempts && class == ErrorTransient {
			status = "dead"
		}
		_ = model.FailSendJob(job.ID, sendErr.Error(), status)
		if emailSendID > 0 {
			_ = model.MarkEmailSendFailed(emailSendID)
		}
		if ShouldSuppressFromError(sendErr) {
			_ = model.SuppressContact(job.ContactID, "bounce", sendErr.Error(), 0)
		}
		if job.CampaignID > 0 {
			reconcileCampaign(job.CampaignID)
		}
		return
	}

	delay := BackoffForAttempt(attempts)
	scheduled := time.Now().Add(delay)
	_ = model.RetrySendJob(job.ID, attempts, scheduled, sendErr.Error())
}
