package outbound

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

// bulkEnqueueDepth pauses the outbound worker while a large campaign is being
// queued so HTTP page loads can still get DB connections (avoids proxy 502s).
var bulkEnqueueDepth atomic.Int32

// BeginBulkEnqueue pauses the send worker until EndBulkEnqueue.
func BeginBulkEnqueue() {
	bulkEnqueueDepth.Add(1)
}

// EndBulkEnqueue resumes the send worker and wakes it once all bulk ops finish.
func EndBulkEnqueue() {
	if bulkEnqueueDepth.Add(-1) == 0 {
		NotifyWorker()
	}
}

func bulkEnqueueActive() bool {
	return bulkEnqueueDepth.Load() > 0
}

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
	// SMTPAccountID forces the From mailbox when > 0 (one-off / explicit sends).
	// Campaign/workflow jobs leave this 0 and use sticky resolution.
	SMTPAccountID int64
	// When TrackingExplicit is true, Open/ClickTrackingEnabled are used as-is.
	// Otherwise campaign defaults apply (one-offs: both off).
	TrackingExplicit       bool
	OpenTrackingEnabled    bool
	ClickTrackingEnabled   bool
	// SubjectPrefix is prepended to the rendered subject at send time (e.g. "[Test]").
	// Encoded on the send job only so email_sends keeps a clean variant label.
	SubjectPrefix string
	// SkipNotify avoids waking the worker on every row during bulk campaign enqueue.
	SkipNotify bool
	// AccountPrechecked skips the per-row "send ready" lookup when the caller already validated.
	AccountPrechecked bool
}

const testSubjectJobVariantPrefix = "test|"

func EnqueueSend(input EnqueueInput) (int64, int64, error) {
	if !input.AccountPrechecked {
		if _, err := model.GetSendReadyAccountForUser(input.UserID); err != nil {
			return 0, 0, err
		}
	}

	if input.WorkflowInstanceID > 0 && input.TemplateID > 0 {
		if sendID, jobID, ok := model.FindActiveWorkflowEmailSend(input.WorkflowInstanceID, input.TemplateID); ok {
			return sendID, jobID, nil
		}
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

	// Block personalization gaps before queueing (especially workflow steps).
	if input.TemplateID > 0 {
		tmpl, err := model.GetTemplate(input.TemplateID)
		if err != nil {
			return 0, 0, fmt.Errorf("template: %w", err)
		}
		_, contactVars, err := model.GetContact(input.ContactID)
		if err != nil {
			return 0, 0, fmt.Errorf("contact: %w", err)
		}
		if missing := util.MissingContactVarsForTemplates(contactVars, tmpl.Subject, tmpl.Body); len(missing) > 0 {
			return 0, 0, fmt.Errorf("missing template variables: %s", strings.Join(missing, ", "))
		}
	}

	// Pin mailbox: explicit one-off choice, else sticky resolution for the contact.
	pinID := int64(0)
	if input.SMTPAccountID > 0 {
		acc, err := model.GetSMTPAccount(input.SMTPAccountID)
		if err != nil || acc.UserID != input.UserID {
			return 0, 0, fmt.Errorf("mailbox not found")
		}
		if !acc.IsSendReady() {
			return 0, 0, fmt.Errorf("mailbox is not ready to send")
		}
		_ = model.EnsureDailyCounterReset(acc.ID)
		pinID = acc.ID
	} else if pinAcc, pinErr := ResolveSendAccountForContactInCampaign(input.UserID, input.ContactID, input.CampaignID); pinErr == nil {
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
	openTrack, clickTrack := resolveEnqueueTracking(input)
	_ = model.SetEmailSendTrackingFlags(emailSendID, openTrack, clickTrack)
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

	if !input.SkipNotify && !bulkEnqueueActive() {
		NotifyWorker()
	}
	return emailSendID, jobID, nil
}

func sendPriority(input EnqueueInput) int {
	if input.CampaignID == 0 && input.WorkflowInstanceID == 0 {
		return PriorityManual
	}
	return PriorityCampaign
}

func resolveEnqueueTracking(input EnqueueInput) (open, click bool) {
	if input.TrackingExplicit {
		return input.OpenTrackingEnabled, input.ClickTrackingEnabled
	}
	if input.CampaignID > 0 {
		return model.CampaignOpenTrackingEnabled(input.CampaignID), model.CampaignClickTrackingEnabled(input.CampaignID)
	}
	return false, false
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
	openTrack := model.CampaignOpenTrackingEnabled(campaignID)
	clickTrack := model.CampaignClickTrackingEnabled(campaignID)

	BeginBulkEnqueue()
	defer EndBulkEnqueue()

	for i, contactID := range allowed {
		templateID, variant := templateForContact(contactID, i)
		_, _, err := EnqueueSend(EnqueueInput{
			UserID:               userID,
			ContactID:            contactID,
			TemplateID:           templateID,
			CampaignID:           campaignID,
			Variant:              variant,
			TrackingExplicit:     true,
			OpenTrackingEnabled:  openTrack,
			ClickTrackingEnabled: clickTrack,
			SkipNotify:           true,
			AccountPrechecked:    true,
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
		// Yield so campaign-detail / other HTTP requests can borrow DB connections.
		if result.Queued%25 == 0 {
			time.Sleep(20 * time.Millisecond)
		}
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
	case strings.Contains(msg, "missing template variables"):
		return "missing_vars"
	case strings.Contains(msg, "gmail"), strings.Contains(msg, "connect gmail"), strings.Contains(msg, "sending profile"):
		return "gmail_not_ready"
	default:
		return "enqueue_error"
	}
}
