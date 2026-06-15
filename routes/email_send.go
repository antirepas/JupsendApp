package routes

import (
	"fmt"
	"log"
	"net/http"

	"emailtracker.com/config"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

var (
	getTemplate    = model.GetTemplate
	getContact     = model.GetContact
	saveSendEmail  = model.SaveSendEmail
)

var newEmailSender = func() MailSender {
	return util.NewEmailSender(
		config.SMTPHost,
		config.SMTPPort,
		config.SMTPUser,
		config.SMTPPass,
		config.SMTPFrom,
	)
}

type MailSender interface {
	Send(to, subject, plainBody, htmlBody string) error
}

func processAndSendEmail(templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
	trackID := fmt.Sprintf("%d", util.GenerateID())

	template, err := getTemplate(templateID)
	if err != nil {
		return 0, fmt.Errorf("could not get template: %w", err)
	}

	contact, contactVars, err := getContact(contactID)
	if err != nil {
		return 0, fmt.Errorf("could not get contact: %w", err)
	}

	emailSendID, err := saveSendEmail(templateID, contactID, trackID, campaignID, variant, workflowInstanceID)
	if err != nil {
		return 0, fmt.Errorf("could not save email send: %w", err)
	}

	newBody, _ := util.RenderTemplate(template.Body, contactVars, "")
	newBody = util.WrapHTMLBody(newBody)
	newBody = util.InjectTrackingPixel(newBody, trackID)
	newSubject, _ := util.RenderTemplate(template.Subject, contactVars, "")
	replacedLinksBody := util.RewriteLinks(newBody, emailSendID)
	plainBody := util.StripHTML(replacedLinksBody)

	if config.SMTPUser == "" || config.SMTPPass == "" {
		return 0, fmt.Errorf("could not send email: SMTP_USER and APP_PASSWORD must be set in .env")
	}

	emailSender := newEmailSender()
	err = emailSender.Send(contact.Email, newSubject, plainBody, replacedLinksBody)
	if err != nil {
		return 0, fmt.Errorf("could not send email: %w", err)
	}

	recordSendContactEvent(contactID, campaignID, workflowInstanceID, emailSendID, templateID)

	return emailSendID, nil
}

func recordSendContactEvent(contactID, campaignID, workflowInstanceID, emailSendID, templateID int64) {
	var wfID int64
	if workflowInstanceID > 0 {
		inst, err := model.GetWorkflowInstance(workflowInstanceID)
		if err == nil {
			v, _ := model.GetWorkflowVersion(inst.WorkflowVersionID)
			wfID = v.WorkflowID
		}
	}
	_, _ = model.InsertContactEvent(model.ContactEventInput{
		ContactID:          contactID,
		CampaignID:         campaignID,
		WorkflowID:         wfID,
		WorkflowInstanceID: workflowInstanceID,
		EmailSendID:        emailSendID,
		EventType:          "SEND",
		Metadata: map[string]interface{}{
			"template_id": templateID,
		},
	})
}

func Email_send(ctx *gin.Context) {
	var emailSend model.EmailSend

	err := ctx.ShouldBindJSON(&emailSend)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "could not get request body"})
		return
	}

	emailSendID, err := processAndSendEmail(emailSend.TemplateID, emailSend.ContactID, 0, "", 0)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":       "email sent successfully!",
		"email_send_id": emailSendID,
	})
}
