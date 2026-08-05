package outbound

import (
	"context"
	"fmt"
	"log"
	"strings"
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

	deliveryStatus, err := model.GetEmailSendDeliveryStatus(emailSendID)
	if err != nil {
		return fmt.Errorf("email send: %w", err)
	}
	switch deliveryStatus {
	case "sent":
		if err := model.ReconcileEmailSendAlreadySent(emailSendID, account.ID, job.ID); err != nil {
			return err
		}
		onJobCompleted(job, emailSendID)
		return nil
	case "failed":
		_ = model.FailSendJob(job.ID, "email send already failed", "failed")
		return nil
	case "queued":
		ok, err := model.TryMarkEmailSendSending(emailSendID)
		if err != nil {
			return err
		}
		if !ok {
			_ = model.RescheduleSendJob(job.ID, time.Now().Add(15*time.Second), "waiting for send slot")
			return nil
		}
	case "sending":
		// Continue — this processing job owns the in-flight send.
	default:
		return fmt.Errorf("unexpected delivery_status %q", deliveryStatus)
	}

	detail, err := model.GetEmailSendDetail(emailSendID)
	if err != nil {
		return fmt.Errorf("email send: %w", err)
	}
	trackID := detail.TrackingID
	baseURL := model.UserBaseURL(job.UserID)

	renderOpts := util.RenderOptions{
		UserID: job.UserID,
		Ctx:    context.Background(),
	}
	newSubject, newBody, missingRequired, err := util.RenderEmail(template.Subject, template.Body, contactVars, renderOpts)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	if len(missingRequired) > 0 {
		reason := "missing_required_var:" + strings.Join(missingRequired, ",")
		_ = model.MarkEmailSendFailed(emailSendID)
		_ = model.FailSendJob(job.ID, reason, "failed")
		return fmt.Errorf("%s", reason)
	}

	newBody = util.WrapHTMLBody(newBody)
	newBody = util.InjectTrackingPixelWithBase(newBody, trackID, baseURL)
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
	smtpPass := account.SMTPPassword
	if account.MailboxSource == "inboxkit" || account.MailboxSource == model.MailboxSourceShared {
		dec, err := model.DecryptSMTPPassword(account)
		if err != nil {
			return fmt.Errorf("mailbox credentials: %w", err)
		}
		smtpPass = dec
	}
	sender := util.NewEmailSender(account.SMTPHost, account.SMTPPort, account.SMTPUser, smtpPass, from)
	messageID := fmt.Sprintf("<%s@%s>", trackID, messageIDDomain(from))

	thread, _ := model.ResolveOutboundThread(job.UserID, job.ContactID, job.WorkflowInstanceID, job.CampaignID, emailSendID)
	inReplyTo := thread.InReplyTo
	references := thread.References
	if thread.HasPrior {
		// Keep the sequence in one inbox thread (classic follow-up style).
		newSubject = model.FollowUpSubject(thread.RootSubject, newSubject)
	}

	meta := util.SendMeta{
		MessageID:          messageID,
		EmailTrackerSendID: fmt.Sprintf("%d", emailSendID),
		FromName:           account.FromName,
		InReplyTo:          inReplyTo,
		References:         references,
	}
	var sendErr error
	if account.IsGoogleOAuth() {
		token, err := model.GmailAccessToken(account)
		if err != nil {
			return fmt.Errorf("gmail oauth: %w", err)
		}
		sendErr = sender.SendWithMetaOAuth(contact.Email, newSubject, plainBody, replacedLinksBody, meta, token)
	} else {
		sendErr = sender.SendWithMeta(contact.Email, newSubject, plainBody, replacedLinksBody, meta)
	}
	if sendErr != nil {
		return sendErr
	}
	log.Printf("outbound: email_send=%d delivered to %s from %s via %s", emailSendID, contact.Email, from, sendTransport(account))

	if err := model.MarkEmailSendSent(emailSendID, account.ID, job.ID); err != nil {
		return err
	}
	_ = model.SaveEmailSendRenderedContent(emailSendID, newSubject, replacedLinksBody, plainBody)
	_, _ = model.InsertConversationMessage(model.ConversationMessageInput{
		UserID:        job.UserID,
		ContactID:     job.ContactID,
		SMTPAccountID: account.ID,
		EmailSendID:   emailSendID,
		Direction:     model.ConversationOutbound,
		FromEmail:     from,
		ToEmail:       contact.Email,
		Subject:       newSubject,
		BodyText:      plainBody,
		BodyHTML:      replacedLinksBody,
		MessageID:     messageID,
		InReplyTo:     inReplyTo,
		OccurredAt:    time.Now(),
	})
	if err := model.CompleteSendJob(job.ID, account.ID); err != nil {
		return err
	}
	MarkAccountSent(account.ID)

	recordSendContactEvent(job.ContactID, job.CampaignID, job.WorkflowInstanceID, emailSendID, job.TemplateID)
	onJobCompleted(job, emailSendID)

	return nil
}

func messageIDDomain(from string) string {
	if i := strings.LastIndex(from, "@"); i >= 0 && i < len(from)-1 {
		return from[i+1:]
	}
	return "localhost"
}

func sendTransport(account model.SMTPAccount) string {
	if account.MailboxSource == model.MailboxSourceShared {
		return "smtp-shared"
	}
	if account.MailboxSource == "inboxkit" {
		return "smtp-inboxkit"
	}
	if account.IsGoogleOAuth() {
		return "smtp-xoauth2"
	}
	return "smtp"
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
	if emailSendID > 0 {
		_ = model.ResetEmailSendForRetry(emailSendID)
	}
}
