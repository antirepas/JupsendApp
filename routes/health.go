package routes

import (
	"fmt"
	"net/http"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
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
	from, err := runUserSMTPCheck(mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "from": from, "version": config.BuildVersion})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "from": from, "version": config.BuildVersion})
}

func runUserSMTPCheck(userID int64) (from string, err error) {
	if userID == 0 {
		return "", fmt.Errorf("not logged in")
	}
	acc, err := model.GetSendReadyAccountForUser(userID)
	if err != nil {
		return "", err
	}
	from = acc.SenderEmail()
	if from == "" {
		return "", fmt.Errorf("no sender email configured")
	}
	if !acc.IsGoogleOAuth() {
		return from, fmt.Errorf("connect Gmail in Settings first")
	}
	token, err := model.GmailAccessToken(acc)
	if err != nil {
		return from, err
	}
	if err := googleoauth.ProbeGmailAPI(token); err != nil {
		return from, err
	}
	return from, nil
}
