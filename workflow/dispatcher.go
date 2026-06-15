package workflow

import (
	"log"
	"strings"

	"emailtracker.com/model"
)

var globalEngine *Engine

func SetEngine(e *Engine) {
	globalEngine = e
}

func GetEngine() *Engine {
	return globalEngine
}

func DispatchContactEvent(contactID int64, eventType string) {
	if globalEngine == nil {
		return
	}
	normalized := strings.ToUpper(eventType)
	if normalized == "OPEN" || normalized == "CLICK" {
		ids, err := model.WakeInstancesForContactEvent(contactID, normalized)
		if err != nil {
			log.Printf("dispatcher: %v", err)
			return
		}
		for _, id := range ids {
			if err := globalEngine.ProcessInstance(id); err != nil {
				log.Printf("dispatcher instance %d: %v", id, err)
			}
		}
	}
}
