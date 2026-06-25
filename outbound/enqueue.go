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
}

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

	trackID := fmt.Sprintf("%d", util.GenerateID())
	emailSendID, err := model.CreateQueuedEmailSend(
		input.UserID, input.TemplateID, input.ContactID, trackID,
		input.CampaignID, input.Variant, input.WorkflowInstanceID,
	)
	if err != nil {
		return 0, 0, err
	}

	jobID, err := model.CreateSendJob(model.SendJob{
		UserID:             input.UserID,
		ContactID:          input.ContactID,
		TemplateID:         input.TemplateID,
		CampaignID:         input.CampaignID,
		Variant:            input.Variant,
		WorkflowInstanceID: input.WorkflowInstanceID,
		EmailSendID:        emailSendID,
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

	return emailSendID, jobID, nil
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
			if strings.Contains(err.Error(), "suppressed") {
				result.SkippedReasons[model.SkipReasonSuppressed]++
			}
			continue
		}
		result.Queued++
	}
	return result, nil
}
