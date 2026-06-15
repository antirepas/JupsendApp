package routes

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func Dashboard(ctx *gin.Context) {
	userID := mustUserID(ctx)
	config.Reload()
	user, _ := model.GetUserByID(userID)
	baseURL := user.BaseURL
	if baseURL == "" {
		baseURL = config.BaseURL
	}

	stats, err := model.GetDashboardStats(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "dashboard", "error": "Failed to load stats"})
		return
	}

	recent, err := model.GetRecentEvents(userID, 10)
	if err != nil {
		log.Print(err)
	}

	daily, err := model.GetDailyStats(userID, 14)
	if err != nil {
		log.Print(err)
	}

	counts, _ := model.GetEntityCounts(userID)
	campaigns, _ := model.ListCampaigns(userID)

	ctx.HTML(http.StatusOK, "dashboard.html", gin.H{
		"title":            "Dashboard",
		"active":           "dashboard",
		"stats":            stats,
		"recentEvents":     recent,
		"dailyStats":       daily,
		"counts":           counts,
		"campaigns":        campaigns,
		"baseURL":          baseURL,
		"trackingWarning":  config.TrackingWarning(baseURL),
		"success":          ctx.Query("success"),
		"error":            ctx.Query("error"),
	})
}

func ListTemplatesPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, err := model.ListTemplates(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to load templates"})
		return
	}
	ctx.HTML(http.StatusOK, "templates_list.html", gin.H{
		"title":     "Templates",
		"active":    "templates",
		"templates": templates,
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewTemplatePage(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":  "New Template",
		"active": "templates",
		"isNew":  true,
	})
}

func CreateTemplate(ctx *gin.Context) {
	userID := mustUserID(ctx)
	name := ctx.PostForm("name")
	subject := ctx.PostForm("subject")
	body := ctx.PostForm("body")
	variables := parseLines(ctx.PostForm("variables"))

	t := model.Template{Name: name, Subject: subject, Body: body}
	tv := make([]model.TemplateVariable, len(variables))
	for i, v := range variables {
		tv[i] = model.TemplateVariable{Key: v}
	}

	_, err := t.SaveTemplate(userID, tv)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to save template"})
		return
	}
	ctx.Redirect(http.StatusFound, "/templates?success=Template+created")
}

func EditTemplatePage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Invalid template ID"})
		return
	}

	t, vars, err := model.GetTemplateByID(id, userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Template not found"})
		return
	}

	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":     "Edit Template",
		"active":    "templates",
		"isNew":     false,
		"template":  t,
		"variables": strings.Join(vars, "\n"),
	})
}

func UpdateTemplate(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Invalid template ID"})
		return
	}

	name := ctx.PostForm("name")
	subject := ctx.PostForm("subject")
	body := ctx.PostForm("body")
	variables := parseLines(ctx.PostForm("variables"))

	err = model.UpdateTemplate(id, userID, name, subject, body, variables)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to update template"})
		return
	}
	ctx.Redirect(http.StatusFound, "/templates?success=Template+updated")
}

func ListContactsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contacts, err := model.ListContacts(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to load contacts"})
		return
	}

	templates, err := model.ListTemplates(userID)
	if err != nil {
		log.Print(err)
	}

	ctx.HTML(http.StatusOK, "contacts_list.html", gin.H{
		"title":     "Contacts",
		"active":    "contacts",
		"contacts":  contacts,
		"templates": templates,
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewContactPage(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "contacts_form.html", gin.H{
		"title":  "New Contact",
		"active": "contacts",
	})
}

func CreateContact(ctx *gin.Context) {
	userID := mustUserID(ctx)
	email := ctx.PostForm("email")
	keys := ctx.PostFormArray("var_key")
	values := ctx.PostFormArray("var_value")

	var cvs []model.ContactVariables
	for i, key := range keys {
		if key == "" {
			continue
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		cvs = append(cvs, model.ContactVariables{Key: key, Value: value})
	}

	c := model.Contact{Email: email}
	_, err := c.SaveContact(userID, cvs)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to save contact"})
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts?success=Contact+created")
}

func PasteContactsQuick(ctx *gin.Context) {
	userID := mustUserID(ctx)
	paste := ctx.PostForm("paste")
	lines := strings.Split(paste, "\n")
	added := 0
	for _, line := range lines {
		email := strings.TrimSpace(line)
		if !strings.Contains(email, "@") {
			continue
		}
		_, err := model.FindOrCreateContact(userID, email, nil)
		if err != nil {
			log.Print(err)
			continue
		}
		added++
	}
	msg := fmt.Sprintf("Added %d contacts", added)
	ctx.Redirect(http.StatusFound, "/contacts?success="+msg)
}

func ListSendsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	sends, err := model.ListEmailSends(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Failed to load sends"})
		return
	}
	ctx.HTML(http.StatusOK, "sends_list.html", gin.H{
		"title":   "Sends",
		"active":  "sends",
		"sends":   sends,
		"success": ctx.Query("success"),
	})
}

func NewSendPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, err := model.ListTemplates(userID)
	if err != nil {
		log.Print(err)
	}
	contacts, err := model.ListContacts(userID)
	if err != nil {
		log.Print(err)
	}
	ctx.HTML(http.StatusOK, "send_form.html", gin.H{
		"title":     "Send Email",
		"active":    "sends",
		"templates": templates,
		"contacts":  contacts,
		"error":     ctx.Query("error"),
	})
}

func CreateSend(ctx *gin.Context) {
	templateID, err := strconv.ParseInt(ctx.PostForm("template_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/sends/new?error=Invalid+template")
		return
	}
	contactID, err := strconv.ParseInt(ctx.PostForm("contact_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/sends/new?error=Invalid+contact")
		return
	}

	emailSendID, err := processAndSendEmail(mustUserID(ctx), templateID, contactID, 0, "", 0)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/sends/new?error="+err.Error())
		return
	}
	ctx.Redirect(http.StatusFound, "/sends/"+strconv.FormatInt(emailSendID, 10)+"?success=Email+queued+for+delivery")
}

func SendDetailPage(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Invalid send ID"})
		return
	}

	detail, err := model.GetEmailSendDetailForUser(id, mustUserID(ctx))
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Send not found"})
		return
	}

	ctx.HTML(http.StatusOK, "sends_detail.html", gin.H{
		"title":  "Send Detail",
		"active": "sends",
		"send":   detail,
		"success": ctx.Query("success"),
	})
}

func parseLines(s string) []string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
