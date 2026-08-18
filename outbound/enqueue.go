package outbound

import (
	"fmt"
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

type EnqueueResult struct {
	Queued          int
	Skipped         int
	SkippedReasons  map[string]int
	JobIDs          []int64
}

type EnqueueInput struct {
	UserID             int64
	ContactID          int64
	TemplateID         int64
	CampaignID         int64
	Variant            string
	WorkflowInstanceID int64
	// SubjectPrefix is prepended to the rendered subject at send time (e.g. "[Test]").
	// Encoded on the send job only so email_sends keeps a clean variant label.
	SubjectPrefix string
}

const testSubjectJobVariantPrefix = "test|"

func EnqueueSend(input EnqueueInput) (int64, int64, error) {
	if _, err := model.GetSendReadyAccountForUser(input.UserID); err != nil {
		return 0, 0, err
	}

	suppressed, err := model.IsContactSuppressed(input.ContactID)
	if err != nil {
		return 0, 0, err
	}
	if suppressed {
		return 0, 0, fmt.Errorf("contact is suppressed")
	}

	emailStatus, _, err := model.GetContactEmailStatus(input.ContactID)
	if err != nil {
		return 0, 0, err
	}
	if emailStatus == "invalid" {
		return 0, 0, fmt.Errorf("contact has invalid email")
	}

	// Pin sticky mailbox up front so concurrent / follow-up jobs keep the same From.
	pinAcc, pinErr := ResolveSendAccountForContact(input.UserID, input.ContactID)
	pinID := int64(0)
	if pinErr == nil {
		pinID = pinAcc.ID
	}

	trackID := fmt.Sprintf("%d", util.GenerateID())
	emailSendID, err := model.CreateQueuedEmailSend(
		input.UserID, input.TemplateID, input.ContactID, trackID,
		input.CampaignID, input.Variant, input.WorkflowInstanceID,
	)
	if err != nil {
		return 0, 0, err
	}
	if pinID > 0 {
		_ = model.PinEmailSendSMTPAccount(emailSendID, pinID)
	}

	jobVariant := input.Variant
	if prefix := strings.TrimSpace(input.SubjectPrefix); prefix != "" {
		jobVariant = testSubjectJobVariantPrefix + input.Variant
	}

	jobID, err := model.CreateSendJob(model.SendJob{
		UserID:             input.UserID,
		SMTPAccountID:      pinID,
		ContactID:          input.ContactID,
		TemplateID:         input.TemplateID,
		CampaignID:         input.CampaignID,
		Variant:            jobVariant,
		WorkflowInstanceID: input.WorkflowInstanceID,
		EmailSendID:        emailSendID,
		Priority:           sendPriority(input),
	})
	if err != nil {
		return 0, 0, err
	}

	if err := model.LinkSendJobEmailSend(jobID, emailSendID); err != nil {
		return 0, 0, err
	}

	if err := model.LinkEmailSendJob(emailSendID, jobID); err != nil {
		return 0, 0, err
	}

	NotifyWorker()
	return emailSendID, jobID, nil
}

func sendPriority(input EnqueueInput) int {
	if input.CampaignID == 0 && input.WorkflowInstanceID == 0 {
		return PriorityManual
	}
	return PriorityCampaign
}

func EnqueueCampaignContacts(userID, campaignID int64, contactIDs []int64, templateForContact func(contactID int64, index int) (templateID int64, variant string)) (EnqueueResult, error) {
	allowed, skippedReasons, err := model.FilterSendEligible(userID, campaignID, contactIDs)
	if err != nil {
		return EnqueueResult{}, err
	}
	result := EnqueueResult{
		Skipped:        len(skippedReasons),
		SkippedReasons: model.CountSkipReasons(skippedReasons),
	}
	for i, contactID := range allowed {
		templateID, variant := templateForContact(contactID, i)
		_, _, err := EnqueueSend(EnqueueInput{
			UserID:     userID,
			ContactID:  contactID,
			TemplateID: templateID,
			CampaignID: campaignID,
			Variant:    variant,
		})
		if err != nil {
			result.Skipped++
			reason := enqueueSkipReason(err)
			if reason != "" {
				result.SkippedReasons[reason]++
			}
			continue
		}
		result.Queued++
	}
	return result, nil
}

func enqueueSkipReason(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "suppressed"):
		return model.SkipReasonSuppressed
	case strings.Contains(msg, "invalid email"):
		return model.SkipReasonInvalidEmail
	case strings.Contains(msg, "gmail"), strings.Contains(msg, "connect gmail"), strings.Contains(msg, "sending profile"):
		return "gmail_not_ready"
	default:
		return "enqueue_error"
	}
}
