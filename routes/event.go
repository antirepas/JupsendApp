package routes

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/util"
	"emailtracker.com/workflow"
	"github.com/gin-gonic/gin"
)

var storeEvent = model.StoreEvent
var getOriginalURL = model.GetOriginalURL
var recordEngagementEventFn = recordEngagementEvent

var trackingPixelGIF = mustDecodeTrackingPixel()

func mustDecodeTrackingPixel() []byte {
	gif, err := base64.StdEncoding.DecodeString(
		"R0lGODlhAQABAPAAAP///wAAACH5BAAAAAAALAAAAAABAAEAAAICRAEAOw==",
	)
	if err != nil || len(gif) == 0 {
		panic("invalid tracking pixel GIF")
	}
	return gif
}

func writeTrackingPixel(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	ctx.Header("Pragma", "no-cache")
	ctx.Header("ngrok-skip-browser-warning", "1")
	ctx.Data(http.StatusOK, "image/gif", trackingPixelGIF)
}

func trackResponseHeaders(ctx *gin.Context) {
	ctx.Header("ngrok-skip-browser-warning", "1")
}

func TrackOpen(ctx *gin.Context) {
	trackingId := ctx.Param("id")

	err := storeEvent(trackingId, "open", ctx.Request.UserAgent(), ctx.ClientIP())
	if err != nil {
		log.Printf("track open error for %s: %v", trackingId, err)
	} else {
		log.Printf("track open recorded: id=%s ua=%s", trackingId, ctx.Request.UserAgent())
	}

	recordEngagementEventFn(trackingId, "OPEN", nil)
	writeTrackingPixel(ctx)
}

func TrackClick(ctx *gin.Context) {
	trackingID := ctx.Param("id")

	originalURL, err := getOriginalURL(trackingID)
	if err != nil {
		log.Printf("track click: unknown link id=%s: %v", trackingID, err)
		ctx.String(http.StatusNotFound, "Link not found or expired.")
		return
	}

	dest, ok := util.SafeRedirectURL(originalURL)
	if !ok {
		log.Printf("track click: invalid destination for id=%s url=%q", trackingID, originalURL)
		ctx.String(http.StatusBadRequest, "Invalid link destination.")
		return
	}

	err = storeEvent(trackingID, "click", ctx.Request.UserAgent(), ctx.ClientIP())
	if err != nil {
		log.Print(err)
	}

	recordEngagementEventFn(trackingID, "CLICK", map[string]interface{}{"clicked_url": dest})
	trackResponseHeaders(ctx)
	ctx.Redirect(http.StatusFound, dest)
}

func recordEngagementEvent(trackingID, eventType string, meta map[string]interface{}) {
	sendID, err := model.GetEmailSendIDByTrackingID(trackingID)
	if err != nil {
		// may be link tracking id
		sendID = resolveSendFromLinkTracking(trackingID)
	}
	if sendID == 0 {
		return
	}

	detail, err := model.GetEmailSendDetail(sendID)
	if err != nil {
		return
	}

	dedupe := ""
	if eventType == "OPEN" {
		dedupe = fmt.Sprintf("open:%d:%s", sendID, time.Now().Format("2006-01-02T15"))
	}

	wfInstID := model.GetSendWorkflowInstanceID(sendID)

	var wfID int64
	if wfInstID > 0 {
		inst, err := model.GetWorkflowInstance(wfInstID)
		if err == nil {
			v, _ := model.GetWorkflowVersion(inst.WorkflowVersionID)
			wfID = v.WorkflowID
		}
	}

	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["tracking_id"] = trackingID

	_, _ = model.InsertContactEvent(model.ContactEventInput{
		ContactID:          detail.ContactID,
		EmailSendID:        sendID,
		WorkflowInstanceID: wfInstID,
		WorkflowID:         wfID,
		EventType:          eventType,
		Metadata:           meta,
		DedupeKey:          dedupe,
	})

	workflow.DispatchContactEvent(detail.ContactID, eventType)
}

func resolveSendFromLinkTracking(trackingID string) int64 {
	sendID, err := model.GetSendIDByLinkTracking(trackingID)
	if err != nil {
		return 0
	}
	return sendID
}
