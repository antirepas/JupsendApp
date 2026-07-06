package routes

import (
	"net/http"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func Healthz(ctx *gin.Context) {
	if err := db.Ping(); err != nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": err.Error(), "version": config.BuildVersion})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ok", "version": config.BuildVersion})
}

func OpsQueue(ctx *gin.Context) {
	stats, err := model.GetGlobalSendJobStats()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"pending":                    stats.Pending,
		"processing":                 stats.Processing,
		"dead":                       stats.Dead,
		"failed":                     stats.Failed,
		"oldest_pending_age_seconds": stats.OldestPendingAgeSeconds,
		"worker_max_concurrent":      outbound.MaxConcurrent,
		"version":                    config.BuildVersion,
	})
}

func OpsSMTPCheck(ctx *gin.Context) {
	userID := mustUserID(ctx)
	acc, err := model.GetSendReadyAccountForUser(userID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	from := acc.SenderEmail()
	if from == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "no sender email"})
		return
	}
	if !acc.IsGoogleOAuth() {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Gmail OAuth required for this check"})
		return
	}
	token, err := model.GmailAccessToken(acc)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if err := util.ProbeSMTPAuth(acc.SMTPHost, acc.SMTPPort, from, token); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "from": from})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "from": from, "version": config.BuildVersion})
}
