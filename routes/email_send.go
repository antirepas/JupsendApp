package routes

import (
	"log"
	"net/http"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"github.com/gin-gonic/gin"
)

var enqueueSendFn = func(userID, templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	emailSendID, _, err := outbound.EnqueueSend(outbound.EnqueueInput{
		UserID:             userID,
		ContactID:          contactID,
		TemplateID:         templateID,
		CampaignID:         campaignID,
		Variant:            variant,
		WorkflowInstanceID: workflowInstanceID,
	})
	return emailSendID, err
}

func processAndSendEmail(userID, templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	return enqueueSendFn(userID, templateID, contactID, campaignID, variant, workflowInstanceID)
}

func Email_send(ctx *gin.Context) {
	var emailSend model.EmailSend

	err := ctx.ShouldBindJSON(&emailSend)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "could not get request body"})
		return
	}

	emailSendID, err := enqueueSendFn(mustUserID(ctx), emailSend.TemplateID, emailSend.ContactID, 0, "", 0)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":         "email queued for delivery",
		"email_send_id":   emailSendID,
		"delivery_status": "queued",
	})
}
