package routes

import (
	"sync"
	"time"

	"emailtracker.com/model"
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

func (workflowMailAdapter) SendWorkflowEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	userID, err := model.GetUserIDForContact(contactID)
	if err != nil {
		return 0, err
	}
	return processAndSendEmail(userID, templateID, contactID, campaignID, variant, workflowInstanceID)
}
