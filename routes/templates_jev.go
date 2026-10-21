package routes

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"emailtracker.com/ai"
	"emailtracker.com/config"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

type templateJevReviewRequest struct {
	Subject      string `json:"subject"`
	Body         string `json:"body"`
	TemplateName string `json:"template_name"`
	Product      string `json:"product"`
}

func requireJEV(ctx *gin.Context) (int64, bool) {
	if !config.JEVEnabled() {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Jev is not configured (set JEV_API_KEY)"})
		return 0, false
	}
	userID := mustUserID(ctx)
	cap, remaining, ok := model.AICreditsRemaining(userID)
	if !ok {
		ctx.JSON(http.StatusTooManyRequests, gin.H{
			"error":     "AI credits exhausted",
			"remaining": remaining,
			"cap":       cap,
		})
		return 0, false
	}
	return userID, true
}

// TemplateJevReview runs a TypeSafe Jev cold-email template review.
func TemplateJevReview(ctx *gin.Context) {
	userID, ok := requireJEV(ctx)
	if !ok {
		return
	}
	var req templateJevReviewRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	subject := strings.TrimSpace(req.Subject)
	body := util.NormalizeQuillHTML(req.Body)
	if subject == "" && strings.TrimSpace(util.StripHTML(body)) == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "subject or body is required"})
		return
	}
	if utf8.RuneCountInString(subject) > 500 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "subject is too long"})
		return
	}
	if utf8.RuneCountInString(body) > 50000 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "body is too long"})
		return
	}

	senderEmail, senderName := "", ""
	if acc, err := model.GetSendReadyAccountForUser(userID); err == nil {
		senderEmail = acc.SenderEmail()
		senderName = strings.TrimSpace(acc.FromName)
	}

	vars := util.ExtractTemplateVariables(subject, body)
	result, err := ai.ReviewTemplate(ctx.Request.Context(), ai.NewJevClient(), ai.TemplateReviewInput{
		Subject:      subject,
		BodyPlain:    util.StripHTML(body),
		TemplateName: strings.TrimSpace(req.TemplateName),
		SenderEmail:  senderEmail,
		SenderName:   senderName,
		Variables:    vars,
		Product:      strings.TrimSpace(req.Product),
	})
	if err != nil {
		respondAIError(ctx, err)
		return
	}
	chargeAI(userID)
	_, rem, _ := model.AICreditsRemaining(userID)
	ctx.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"review":    result,
		"remaining": rem,
	})
}
