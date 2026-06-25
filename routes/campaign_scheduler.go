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
		campaign, err := model.GetCampaign(id)
		if err != nil {
			continue
		}
		result, err := launchCampaign(campaign.UserID, id)
		if err != nil {
			log.Printf("scheduler: campaign %d: %v", id, err)
			continue
		}
		log.Printf("scheduler: campaign %d launched (%d queued, %d skipped)", id, result.Queued, result.Skipped)
	}
}
