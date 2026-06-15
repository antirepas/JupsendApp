package routes

import (
	"log"
	"sync"
	"time"

	"emailtracker.com/model"
)

var schedulerMu sync.Mutex

func StartCampaignScheduler() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			runDueScheduledCampaigns()
		}
	}()
}

func runDueScheduledCampaigns() {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()

	ids, err := model.GetDueScheduledCampaignIDs()
	if err != nil {
		log.Printf("scheduler: list due campaigns: %v", err)
		return
	}
	for _, id := range ids {
		sent, failed, err := launchCampaign(id)
		if err != nil {
			log.Printf("scheduler: campaign %d: %v", id, err)
			continue
		}
		log.Printf("scheduler: campaign %d launched (%d ok, %d failed)", id, sent, failed)
	}
}
