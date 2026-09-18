package routes

import (
	"sync"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/workflow"
)

var workflowSchedulerMu sync.Mutex

func StartWorkflowScheduler(engine *workflow.Engine) {
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		for range ticker.C {
			runWorkflowProcessor(engine)
		}
	}()
}

func runWorkflowProcessor(engine *workflow.Engine) {
	if engine == nil {
		return
	}
	workflowSchedulerMu.Lock()
	defer workflowSchedulerMu.Unlock()
	engine.ProcessDueInstances()
}

func InitWorkflowEngine() *workflow.Engine {
	mailer := &workflowMailAdapter{}
	engine := workflow.NewEngine(mailer)
	workflow.SetEngine(engine)
	return engine
}

type workflowMailAdapter struct{}

func (workflowMailAdapter) SendWorkflowEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64, openTracking, clickTracking bool) (int64, error) {
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
		WorkflowInstanceID:   workflowInstanceID,
		TrackingExplicit:     true,
		OpenTrackingEnabled:  openTracking,
		ClickTrackingEnabled: clickTracking,
	})
	return emailSendID, err
}
