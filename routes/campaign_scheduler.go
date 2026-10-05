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
		if campaign.IsSending {
			continue
		}
		if err := model.MarkCampaignSending(id); err != nil {
			log.Printf("scheduler: campaign %d mark sending: %v", id, err)
			continue
		}
		go func(userID, campaignID int64) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("scheduler: campaign %d panic: %v", campaignID, r)
					_ = model.ClearCampaignSending(campaignID)
				}
			}()
			result, err := launchCampaign(userID, campaignID)
			if err != nil {
				log.Printf("scheduler: campaign %d: %v", campaignID, err)
				_ = model.ClearCampaignSending(campaignID)
				return
			}
			log.Printf("scheduler: campaign %d launched (%d queued, %d skipped)", campaignID, result.Queued, result.Skipped)
		}(campaign.UserID, id)
	}
}
