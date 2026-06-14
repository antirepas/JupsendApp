package routes

import (
	"encoding/base64"
	"log"
	"net/http"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

var storeEvent = model.StoreEvent
var getOriginalURL = model.GetOriginalURL

func TrackOpen(ctx *gin.Context) {
	trackingId := ctx.Param("id")

	err := storeEvent(trackingId, "open", ctx.Request.UserAgent(), ctx.ClientIP())
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "could not store event"})
		return
	}

	pixel, _ := base64.StdEncoding.DecodeString(
		"R0lGODlhAQABAPAAAP///wAAACH5BAAAAAAALAAAAAABAAEAAAICRAEAOw==",
	)
	ctx.Header("Content-type", "image/gif")
	ctx.Writer.Write(pixel)
}

func TrackClick(ctx *gin.Context) {
	trackingID := ctx.Param("id")

	originalUrl, err := getOriginalURL(trackingID)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "could not get original url"})
		return
	}

	err = storeEvent(trackingID, "click", ctx.Request.UserAgent(), ctx.ClientIP())
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "could not store event"})
		return
	}

	ctx.Redirect(http.StatusFound, originalUrl)
}
