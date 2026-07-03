package routes

import (
	"net/http"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"github.com/gin-gonic/gin"
)

func Healthz(ctx *gin.Context) {
	if err := db.Ping(); err != nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
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
	})
}
