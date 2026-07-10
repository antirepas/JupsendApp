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
	UseAI   bool              `json:"use_ai"`
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

	userID := mustUserID(ctx)
	opts := util.RenderOptions{
		ForPreview: true,
		UseAI:      req.UseAI,
		UserID:     userID,
		Ctx:        ctx.Request.Context(),
	}
	subj, body, missing, err := util.RenderEmail(req.Subject, req.Body, vars, opts)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "render failed"})
		return
	}
	body = util.WrapHTMLBody(body)

	ctx.JSON(http.StatusOK, gin.H{
		"subject":          subj,
		"body_html":        body,
		"missing_required": missing,
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
