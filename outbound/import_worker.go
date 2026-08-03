package outbound

import (
	"log"
	"strconv"
	"time"

	"emailtracker.com/model"
)

// Small chunks so processed_rows / progress % update often enough for the UI poll.
const importChunkSize = 15

var importWake = make(chan struct{}, 1)

// NotifyImportWorker wakes the import worker (coalesced).
func NotifyImportWorker() {
	select {
	case importWake <- struct{}{}:
	default:
	}
}

// StartImportWorker processes contact/campaign import jobs in the background.
func StartImportWorker() {
	model.ReclaimStaleImportJobs()
	go func() {
		runImportOnce()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
			case <-importWake:
			}
			runImportOnce()
		}
	}()
	log.Printf("import worker started")
}

func runImportOnce() {
	for {
		job, ok, err := model.ClaimNextImportJob()
		if err != nil {
			log.Printf("import claim: %v", err)
			return
		}
		if !ok {
			return
		}
		processImportJob(job)
	}
}

func processImportJob(job model.ImportJob) {
	payload, err := model.DecodeImportPayload(job.PayloadJSON)
	if err != nil {
		_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", "Invalid import payload", model.ImportContactsResult{}, 0)
		return
	}

	switch job.Kind {
	case model.ImportKindCampaignListSnapshot:
		processListSnapshotJob(job, payload)
	default:
		processRowsImportJob(job, payload)
	}
}

func processListSnapshotJob(job model.ImportJob, payload model.ImportJobPayload) {
	listID := payload.SnapshotListID
	campaignID := payload.CampaignID
	if campaignID == 0 {
		campaignID = job.CampaignID
	}
	if _, err := model.GetContactListForUser(listID, job.UserID); err != nil {
		_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", err.Error(), model.ImportContactsResult{}, 0)
		return
	}
	if _, err := model.GetCampaignForUser(campaignID, job.UserID); err != nil {
		_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", err.Error(), model.ImportContactsResult{}, 0)
		return
	}
	ids, err := model.ListMemberContactIDs(listID, job.UserID)
	if err != nil {
		_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", err.Error(), model.ImportContactsResult{}, 0)
		return
	}
	if err := model.UpdateImportJobProgress(job.ID, 0, 0, 0, 0, 0); err != nil {
		log.Printf("import job %d progress: %v", job.ID, err)
	}
	processed := 0
	for i := 0; i < len(ids); i += importChunkSize {
		end := i + importChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := model.AddContactsToCampaign(campaignID, ids[i:end]); err != nil {
			_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", err.Error(), model.ImportContactsResult{Created: processed}, processed)
			return
		}
		processed = end
		if err := model.UpdateImportJobProgress(job.ID, processed, processed, 0, 0, 0); err != nil {
			log.Printf("import job %d progress: %v", job.ID, err)
		}
	}
	_ = model.SetCampaignContactList(campaignID, job.UserID, listID)
	result := model.ImportContactsResult{Created: processed}
	msg := "Added " + strconv.Itoa(processed) + " contacts from list"
	_ = model.FinishImportJob(job.ID, model.ImportStatusDone, msg, "", result, processed)
}

func processRowsImportJob(job model.ImportJob, payload model.ImportJobPayload) {
	rows := payload.Rows
	keys := payload.ImportKeys
	listID := payload.ListID
	if listID == 0 {
		listID = job.ListID
	}
	campaignID := payload.CampaignID
	if campaignID == 0 {
		campaignID = job.CampaignID
	}

	if err := model.UpdateImportJobProgress(job.ID, 0, 0, 0, 0, 0); err != nil {
		log.Printf("import job %d progress: %v", job.ID, err)
	}

	var result model.ImportContactsResult
	processed := 0
	for i := 0; i < len(rows); i += importChunkSize {
		end := i + importChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		chunkResult, err := model.ImportContactRows(job.UserID, rows[i:end], listID, keys)
		if err != nil {
			_ = model.FinishImportJob(job.ID, model.ImportStatusFailed, "", err.Error(), result, processed)
			return
		}
		result.Created += chunkResult.Created
		result.Updated += chunkResult.Updated
		result.Skipped += chunkResult.Skipped
		result.Errors += chunkResult.Errors
		result.ContactIDs = append(result.ContactIDs, chunkResult.ContactIDs...)
		if len(result.InvalidEmails) < 10 {
			result.InvalidEmails = append(result.InvalidEmails, chunkResult.InvalidEmails...)
			if len(result.InvalidEmails) > 10 {
				result.InvalidEmails = result.InvalidEmails[:10]
			}
		}
		processed = end
		if err := model.UpdateImportJobProgress(job.ID, processed, result.Created, result.Updated, result.Skipped, result.Errors); err != nil {
			log.Printf("import job %d progress: %v", job.ID, err)
		}

		if campaignID > 0 && len(chunkResult.ContactIDs) > 0 {
			if err := model.AddContactsToCampaign(campaignID, chunkResult.ContactIDs); err != nil {
				log.Printf("import job %d add to campaign: %v", job.ID, err)
			}
		}
	}

	msg := model.FormatImportResultMessage(result)
	if campaignID > 0 {
		msg = msg + " · linked to campaign"
	}
	_ = model.FinishImportJob(job.ID, model.ImportStatusDone, msg, "", result, processed)
}
