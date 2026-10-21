package sequence

import (
	"log"
	"sync"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
)

var (
	engineMu sync.Mutex
	mailer   Mailer = defaultMailer{}
)

// Mailer enqueues sequence emails.
type Mailer interface {
	SendSequenceEmail(templateID, contactID, campaignID int64, variant string, sequenceInstanceID int64, openTracking, clickTracking bool) (int64, error)
}

type defaultMailer struct{}

func (defaultMailer) SendSequenceEmail(templateID, contactID, campaignID int64, variant string, sequenceInstanceID int64, openTracking, clickTracking bool) (int64, error) {
	userID, err := model.GetUserIDForContact(contactID)
	if err != nil {
		return 0, err
	}
	emailSendID, _, err := outbound.EnqueueSend(outbound.EnqueueInput{
		UserID:               userID,
		ContactID:            contactID,
		TemplateID:           templateID,
		CampaignID:           campaignID,
		Variant:              variant,
		SequenceInstanceID:   sequenceInstanceID,
		TrackingExplicit:     true,
		OpenTrackingEnabled:  openTracking,
		ClickTrackingEnabled: clickTracking,
	})
	return emailSendID, err
}

// SetMailer overrides the enqueue adapter (tests).
func SetMailer(m Mailer) {
	if m == nil {
		mailer = defaultMailer{}
		return
	}
	mailer = m
}

// ProcessDueInstances advances due sequence contacts.
func ProcessDueInstances() {
	engineMu.Lock()
	defer engineMu.Unlock()

	insts, err := model.ClaimDueSequenceInstances(80)
	if err != nil {
		log.Printf("sequence claim: %v", err)
		return
	}
	for _, inst := range insts {
		if err := processInstance(inst); err != nil {
			log.Printf("sequence instance %d: %v", inst.ID, err)
		}
	}
}

func processInstance(inst model.CampaignSequenceInstance) error {
	camp, err := model.GetCampaign(inst.CampaignID)
	if err != nil {
		_ = model.CancelSequenceInstance(inst.ID)
		return err
	}
	if camp.Status == "stopped" {
		return model.CancelSequenceInstance(inst.ID)
	}
	if block, _ := model.ShouldBlockWorkflowSend(inst.CampaignID, inst.ContactID); block {
		return model.CancelSequenceInstance(inst.ID)
	}

	steps, err := model.ListCampaignSequenceSteps(inst.CampaignID)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return model.UpdateSequenceInstance(inst.ID, "completed", inst.StepIndex, inst.LastEmailSendID, nil)
	}
	if inst.StepIndex < 0 {
		inst.StepIndex = 0
	}
	if inst.StepIndex >= len(steps) {
		return model.UpdateSequenceInstance(inst.ID, "completed", inst.StepIndex, inst.LastEmailSendID, nil)
	}

	step := steps[inst.StepIndex]

	// Active + delay: park once. Waiting instances are already due — send.
	if inst.Status == "active" && step.DelaySecondsBefore > 0 {
		wake := time.Now().Add(time.Duration(step.DelaySecondsBefore) * time.Second)
		return model.UpdateSequenceInstance(inst.ID, "waiting", inst.StepIndex, inst.LastEmailSendID, &wake)
	}

	tmplID, err := model.ResolveSequenceStepTemplate(camp, step, inst.StepIndex, inst.Variant)
	if err != nil {
		return err
	}

	openTrack := camp.OpenTrackingEnabled
	clickTrack := camp.ClickTrackingEnabled
	sendID, err := mailer.SendSequenceEmail(tmplID, inst.ContactID, camp.ID, inst.Variant, inst.ID, openTrack, clickTrack)
	if err != nil {
		// Soft-fail: cancel this contact rather than spinning forever.
		_ = model.UpdateSequenceInstance(inst.ID, "failed", inst.StepIndex, inst.LastEmailSendID, nil)
		return err
	}

	nextIndex := inst.StepIndex + 1
	if nextIndex >= len(steps) {
		return model.UpdateSequenceInstance(inst.ID, "completed", nextIndex, sendID, nil)
	}
	next := steps[nextIndex]
	if next.DelaySecondsBefore > 0 {
		wake := time.Now().Add(time.Duration(next.DelaySecondsBefore) * time.Second)
		return model.UpdateSequenceInstance(inst.ID, "waiting", nextIndex, sendID, &wake)
	}
	// Immediate next send — stay active at next index for another pass soon.
	return model.UpdateSequenceInstance(inst.ID, "active", nextIndex, sendID, nil)
}
