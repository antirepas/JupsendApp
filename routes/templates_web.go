package routes

import (
	"encoding/json"
	"net/http"

	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

type templatePreviewRequest struct {
	Subject string            `json:"subject"`
	Body    string            `json:"body"`
	Sample  map[string]string `json:"sample"`
}

func PreviewTemplate(ctx *gin.Context) {
	var req templatePreviewRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	vars := make([]model.ContactVariables, 0, len(req.Sample))
	for k, v := range req.Sample {
		vars = append(vars, model.ContactVariables{Key: k, Value: v})
	}

	subj, _ := util.RenderTemplate(req.Subject, vars, "")
	body, _ := util.RenderTemplate(req.Body, vars, "")
	body = util.WrapHTMLBody(body)

	ctx.JSON(http.StatusOK, gin.H{
		"subject":   subj,
		"body_html": body,
	})
}

func templateBuilderContext(userID int64) (senderEmail string, defaultSampleJSON string) {
	user, _ := model.GetUserByID(userID)
	senderEmail = user.Email
	if acc, err := model.GetActiveSMTPAccountForUser(userID); err == nil {
		switch {
		case acc.GoogleEmail != "":
			senderEmail = acc.GoogleEmail
		case acc.FromEmail != "":
			senderEmail = acc.FromEmail
		case acc.SMTPUser != "":
			senderEmail = acc.SMTPUser
		}
	}
	sample := map[string]string{
		"name":    "Alex",
		"company": "Acme Corp",
	}
	b, _ := json.Marshal(sample)
	return senderEmail, string(b)
}
