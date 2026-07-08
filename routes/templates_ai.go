package routes

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"emailtracker.com/ai"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

var templateAICompleter ai.Completer = ai.DefaultCompleter

type templateLintRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func TemplateLint(ctx *gin.Context) {
	var req templateLintRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	issues := util.LintTemplate(req.Subject, req.Body)
	ctx.JSON(http.StatusOK, gin.H{"lint": issues})
}

type templateAIRewriteRequest struct {
	Text   string `json:"text"`
	Action string `json:"action"`
}

func requireAI(ctx *gin.Context) (int64, bool) {
	if !ai.Enabled() {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured"})
		return 0, false
	}
	userID := mustUserID(ctx)
	cap, remaining, ok := model.ConsumeAICredit(userID)
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

func TemplateAIRewrite(ctx *gin.Context) {
	if !ai.Enabled() {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured"})
		return
	}
	var req templateAIRewriteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}
	if utf8.RuneCountInString(text) > 50 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "text must be 50 characters or fewer"})
		return
	}
	action := strings.TrimSpace(req.Action)
	switch action {
	case "shorten", "soften", "direct", "grammar":
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid action"})
		return
	}
	if _, ok := requireAI(ctx); !ok {
		return
	}

	out, err := templateAICompleter.Complete(ctx.Request.Context(), ai.RewritePrompt(action), text)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "AI request failed"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"text": strings.TrimSpace(out)})
}

type templateAISubjectRequest struct {
	Subject   string   `json:"subject"`
	Variables []string `json:"variables"`
}

func TemplateAISubjectAlternatives(ctx *gin.Context) {
	if _, ok := requireAI(ctx); !ok {
		return
	}
	var req templateAISubjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	userMsg := "Current subject: " + req.Subject
	if len(req.Variables) > 0 {
		userMsg += "\nVariables: " + strings.Join(req.Variables, ", ")
	}

	out, err := templateAICompleter.Complete(ctx.Request.Context(), ai.SubjectAlternativesPrompt(), userMsg)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "AI request failed"})
		return
	}

	alts := parseJSONArray(out)
	if len(alts) == 0 {
		ctx.JSON(http.StatusOK, gin.H{"alternatives": []string{}})
		return
	}
	if len(alts) > 3 {
		alts = alts[:3]
	}
	ctx.JSON(http.StatusOK, gin.H{"alternatives": alts})
}

type templateAIPersonalizationRequest struct {
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
}

func TemplateAIPersonalizationHint(ctx *gin.Context) {
	if _, ok := requireAI(ctx); !ok {
		return
	}
	var req templateAIPersonalizationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	vars := util.ExtractTemplateVariables(req.Subject, req.Body)
	if len(vars) > 0 {
		ctx.JSON(http.StatusOK, gin.H{"hint": ""})
		return
	}

	userMsg := "Subject: " + req.Subject + "\nBody excerpt: " + truncateRunes(util.StripHTML(req.Body), 300)
	if len(req.Variables) > 0 {
		userMsg += "\nVariables: " + strings.Join(req.Variables, ", ")
	}

	out, err := templateAICompleter.Complete(ctx.Request.Context(), ai.PersonalizationHintPrompt(), userMsg)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "AI request failed"})
		return
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.EqualFold(out, "SKIP") {
		ctx.JSON(http.StatusOK, gin.H{"hint": ""})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"hint": out})
}

type templateAIToneRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func TemplateAIToneCheck(ctx *gin.Context) {
	if _, ok := requireAI(ctx); !ok {
		return
	}
	var req templateAIToneRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	plain := util.StripHTML(req.Body)
	if utf8.RuneCountInString(plain) < 20 {
		ctx.JSON(http.StatusOK, gin.H{"tone": "neutral", "message": ""})
		return
	}

	userMsg := "Subject: " + req.Subject + "\nBody: " + truncateRunes(plain, 500)
	out, err := templateAICompleter.Complete(ctx.Request.Context(), ai.ToneCheckPrompt(), userMsg)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "AI request failed"})
		return
	}

	var parsed struct {
		Tone    string `json:"tone"`
		Message string `json:"message"`
	}
	clean := extractJSONObject(out)
	if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"tone": "neutral", "message": ""})
		return
	}
	parsed.Tone = strings.ToLower(strings.TrimSpace(parsed.Tone))
	if parsed.Tone != "formal" && parsed.Tone != "casual" {
		parsed.Tone = "neutral"
	}
	ctx.JSON(http.StatusOK, gin.H{"tone": parsed.Tone, "message": strings.TrimSpace(parsed.Message)})
}

type templateAIStarter struct {
	Label    string `json:"label"`
	Skeleton string `json:"skeleton"`
}

func TemplateAIStarters(ctx *gin.Context) {
	if !ai.Enabled() {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured"})
		return
	}
	starters := []templateAIStarter{
		{
			Label: "Problem → question",
			Skeleton: `<p>Hi {{name}},</p><p>I noticed {{company}} might be dealing with [specific problem].</p><p>Is that something you're working on right now?</p><p>Best,<br>You</p>`,
		},
		{
			Label: "Compliment → ask",
			Skeleton: `<p>Hi {{name}},</p><p>I liked what {{company}} is doing with [specific detail].</p><p>Quick question — [your ask]?</p><p>Thanks,<br>You</p>`,
		},
		{
			Label: "Mutual connection → ask",
			Skeleton: `<p>Hi {{name}},</p><p>[Mutual connection] suggested I reach out about [topic].</p><p>Would you be open to a quick chat?</p><p>Best,<br>You</p>`,
		},
	}
	ctx.JSON(http.StatusOK, gin.H{"starters": starters})
}

type templateAISoftenBodyRequest struct {
	Body string `json:"body"`
}

func TemplateAISoftenBody(ctx *gin.Context) {
	if _, ok := requireAI(ctx); !ok {
		return
	}
	var req templateAISoftenBodyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "body is required"})
		return
	}

	out, err := templateAICompleter.Complete(ctx.Request.Context(), ai.SoftenBodyPrompt(), body)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "AI request failed"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"body": strings.TrimSpace(out)})
}

func parseJSONArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(raw[start:end+1]), &arr)
	}
	return arr
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	return raw
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= max {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
