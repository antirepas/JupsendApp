package routes

import (
	"net/http"
	"net/url"
	"strconv"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"github.com/gin-gonic/gin"
)

func enqueueContactImport(userID int64, kind string, rows []model.ImportContactRow, listID int64, importKeys []string, redirectBase string) (string, string) {
	if len(rows) == 0 {
		return redirectBase + "&error=" + url.QueryEscape("No contacts to import"), ""
	}
	job, err := model.EnqueueImportJob(userID, kind, model.ImportJobPayload{
		Rows:       rows,
		ImportKeys: importKeys,
		ListID:     listID,
	})
	if err != nil {
		return redirectBase + "&error=" + url.QueryEscape(err.Error()), ""
	}
	outbound.NotifyImportWorker()
	msg := "Import started in the background (" + strconv.Itoa(job.TotalRows) + " rows). You can keep using the app."
	return redirectBase + "&success=" + url.QueryEscape(msg), ""
}

func enqueueCampaignImport(userID, campaignID int64, kind string, rows []model.ImportContactRow, importKeys []string) string {
	base := "/campaigns/" + strconv.FormatInt(campaignID, 10)
	if len(rows) == 0 {
		return base + "?error=" + url.QueryEscape("No contacts to import")
	}
	job, err := model.EnqueueImportJob(userID, kind, model.ImportJobPayload{
		Rows:       rows,
		ImportKeys: importKeys,
		CampaignID: campaignID,
	})
	if err != nil {
		return base + "?error=" + url.QueryEscape(err.Error())
	}
	outbound.NotifyImportWorker()
	msg := "Import started (" + strconv.Itoa(job.TotalRows) + " rows). Progress shows in the top banner."
	return base + "?success=" + url.QueryEscape(msg)
}

func ListImportJobsAPI(ctx *gin.Context) {
	userID := mustUserID(ctx)
	jobs, err := model.ListActiveImportJobsForUser(userID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load imports"})
		return
	}
	type jobView struct {
		ID            int64  `json:"id"`
		Kind          string `json:"kind"`
		KindLabel     string `json:"kind_label"`
		Status        string `json:"status"`
		TotalRows     int    `json:"total_rows"`
		ProcessedRows int    `json:"processed_rows"`
		Progress      int    `json:"progress"`
		CreatedCount  int    `json:"created_count"`
		UpdatedCount  int    `json:"updated_count"`
		SkippedCount  int    `json:"skipped_count"`
		ErrorCount    int    `json:"error_count"`
		Message       string `json:"message"`
		ErrorMessage  string `json:"error_message"`
		CampaignID    int64  `json:"campaign_id"`
	}
	out := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobView{
			ID:            j.ID,
			Kind:          j.Kind,
			KindLabel:     j.KindLabel(),
			Status:        j.Status,
			TotalRows:     j.TotalRows,
			ProcessedRows: j.ProcessedRows,
			Progress:      j.ProgressPercent(),
			CreatedCount:  j.CreatedCount,
			UpdatedCount:  j.UpdatedCount,
			SkippedCount:  j.SkippedCount,
			ErrorCount:    j.ErrorCount,
			Message:       j.Message,
			ErrorMessage:  j.ErrorMessage,
			CampaignID:    j.CampaignID,
		})
	}
	ctx.Header("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, gin.H{"jobs": out})
}

func DismissImportJobAPI(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	_ = model.DismissImportJob(id, userID)
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}
