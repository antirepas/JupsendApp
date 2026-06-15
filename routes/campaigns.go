package routes

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
	"emailtracker.com/workflow"
	"github.com/gin-gonic/gin"
)

func ListCampaignsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaigns, err := model.ListCampaigns(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Failed to load campaigns"})
		return
	}
	ctx.HTML(http.StatusOK, "campaigns_list.html", gin.H{
		"title":     "Campaigns",
		"active":    "campaigns",
		"campaigns": campaigns,
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewCampaignPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, _ := model.ListTemplates(userID)
	workflows, _ := model.GetPublishedWorkflows(userID)
	ctx.HTML(http.StatusOK, "campaigns_form.html", gin.H{
		"title":     "New Campaign",
		"active":    "campaigns",
		"templates": templates,
		"workflows": workflows,
	})
}

func CreateCampaign(ctx *gin.Context) {
	userID := mustUserID(ctx)
	name := ctx.PostForm("name")
	executionMode := ctx.PostForm("execution_mode")
	if executionMode == "" {
		executionMode = "bulk"
	}
	workflowVersionID, _ := strconv.ParseInt(ctx.PostForm("workflow_version_id"), 10, 64)

	templateAID, err := strconv.ParseInt(ctx.PostForm("template_a_id"), 10, 64)
	if executionMode == "bulk" && err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/new?error=Select+template+A")
		return
	}
	if executionMode == "workflow" && workflowVersionID == 0 {
		ctx.Redirect(http.StatusFound, "/campaigns/new?error=Select+a+published+workflow")
		return
	}
	templateBID, _ := strconv.ParseInt(ctx.PostForm("template_b_id"), 10, 64)
	if executionMode == "workflow" {
		templateAID = 1 // placeholder for NOT NULL constraint; unused in workflow mode
	}

	id, err := model.CreateCampaign(userID, name, templateAID, templateBID, executionMode, workflowVersionID)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/campaigns/new?error=Failed+to+create+campaign")
		return
	}
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(id, 10)+"?success=Campaign+created")
}

func CampaignDetailPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Invalid campaign ID"})
		return
	}

	detail, err := model.GetCampaignDetail(id, userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Campaign not found"})
		return
	}

	campaign, _ := model.GetCampaignForUser(id, userID)
	contactIDs, _ := model.GetCampaignContactIDs(id)
	allContacts, _ := model.ListContacts(userID)
	mergedVars, _ := model.MergeTemplateVariables(userID, []int64{detail.TemplateAID, detail.TemplateBID})

	templateA := templatePreviewView(userID, detail.TemplateAID)
	var templateB TemplatePreviewView
	hasB := detail.TemplateBID > 0
	if hasB {
		templateB = templatePreviewView(userID, detail.TemplateBID)
	}

	contactRows := buildCampaignContactRows(campaign, userID, contactIDs, detail.TemplateAName, detail.TemplateBName)

	var scheduledAtLocal string
	if detail.ScheduledAt != nil {
		scheduledAtLocal = detail.ScheduledAt.Format("2006-01-02T15:04")
	}

	ctx.HTML(http.StatusOK, "campaigns_detail.html", gin.H{
		"title":            detail.Name,
		"active":           "campaigns",
		"campaign":         detail,
		"allContacts":      allContacts,
		"variables":        strings.Join(mergedVars, ","),
		"templateA":        templateA,
		"templateB":        templateB,
		"hasB":             hasB,
		"contactRows":      contactRows,
		"mergedVars":       mergedVars,
		"scheduledAtLocal": scheduledAtLocal,
		"success":          ctx.Query("success"),
		"error":            ctx.Query("error"),
	})
}

func CampaignAnalyticsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Invalid campaign ID"})
		return
	}

	analytics, err := model.GetCampaignAnalytics(id, userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Campaign not found"})
		return
	}

	ctx.HTML(http.StatusOK, "campaigns_analytics.html", gin.H{
		"title":     analytics.Name + " Analytics",
		"active":    "campaigns",
		"analytics": analytics,
	})
}

func AddCampaignContacts(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	contactIDs := parseContactIDs(ctx)
	if len(contactIDs) > 0 {
		err = model.AddContactsToCampaign(campaignID, contactIDs)
		if err != nil {
			log.Print(err)
		}
	}

	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success=Contacts+added")
}

func PasteCampaignContacts(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	campaign, err := model.GetCampaignForUser(campaignID, mustUserID(ctx))
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}

	vars, _ := model.MergeTemplateVariables(mustUserID(ctx), []int64{campaign.TemplateAID, campaign.TemplateBID})
	paste := ctx.PostForm("paste")
	rows := util.ParseContactPasteWithHeaders(paste, vars)

	added := 0
	var contactIDs []int64
	for _, row := range rows {
		var cvs []model.ContactVariables
		for _, key := range vars {
			cvs = append(cvs, model.ContactVariables{Key: key, Value: row.Variables[key]})
		}
		cid, err := model.FindOrCreateContact(mustUserID(ctx), row.Email, cvs)
		if err != nil {
			continue
		}
		contactIDs = append(contactIDs, cid)
		added++
	}

	if len(contactIDs) > 0 {
		_ = model.AddContactsToCampaign(campaignID, contactIDs)
	}

	msg := fmt.Sprintf("Added %d contacts", added)
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
}

func UploadCampaignContacts(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	campaign, err := model.GetCampaignForUser(campaignID, mustUserID(ctx))
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}

	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=No+file+uploaded")
		return
	}
	src, err := file.Open()
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Could+not+read+file")
		return
	}
	defer src.Close()

	vars, _ := model.MergeTemplateVariables(mustUserID(ctx), []int64{campaign.TemplateAID, campaign.TemplateBID})
	rows, err := util.ParseContactsExcel(src, vars)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}

	var contactIDs []int64
	for _, row := range rows {
		var cvs []model.ContactVariables
		for _, key := range vars {
			cvs = append(cvs, model.ContactVariables{Key: key, Value: row.Variables[key]})
		}
		cid, err := model.FindOrCreateContact(mustUserID(ctx), row.Email, cvs)
		if err != nil {
			continue
		}
		contactIDs = append(contactIDs, cid)
	}

	if len(contactIDs) > 0 {
		_ = model.AddContactsToCampaign(campaignID, contactIDs)
	}

	msg := fmt.Sprintf("Imported %d contacts", len(contactIDs))
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
}

func SendCampaign(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	sent, failed, err := launchCampaign(mustUserID(ctx), campaignID)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}

	msg := fmt.Sprintf("Campaign queued: %d emails queued, %d suppressed", sent, failed)
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
}

func ScheduleCampaign(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	at, err := parseScheduledAt(ctx.PostForm("scheduled_at"))
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Invalid+date+or+time")
		return
	}
	if !at.After(time.Now()) {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Schedule+time+must+be+in+the+future")
		return
	}

	contactIDs, _ := model.GetCampaignContactIDs(campaignID)
	if len(contactIDs) == 0 {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Add+contacts+before+scheduling")
		return
	}

	if err := model.ScheduleCampaign(campaignID, at); err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Could+not+schedule+campaign")
		return
	}

	msg := fmt.Sprintf("Campaign scheduled for %s", at.Format("Jan 2, 2006 15:04"))
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
}

func CancelCampaignSchedule(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	if err := model.ClearCampaignSchedule(campaignID); err != nil {
		log.Print(err)
	}
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success=Schedule+cancelled")
}

func executeCampaignSend(userID, campaignID int64) (queued, skipped int, err error) {
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("campaign not found")
	}
	if campaign.Status == "sent" {
		return 0, 0, fmt.Errorf("campaign already sent")
	}
	if campaign.IsSending {
		return 0, 0, fmt.Errorf("campaign is already sending")
	}

	contactIDs, err := model.GetCampaignContactIDs(campaignID)
	if err != nil || len(contactIDs) == 0 {
		return 0, 0, fmt.Errorf("no contacts in campaign")
	}

	hasB := campaign.TemplateBID > 0
	result, err := outbound.EnqueueCampaignContacts(userID, campaignID, contactIDs, func(contactID int64, i int) (int64, string) {
		variant := "A"
		templateID := campaign.TemplateAID
		if hasB && i%2 == 1 {
			variant = "B"
			templateID = campaign.TemplateBID
		}
		return templateID, variant
	})
	if err != nil {
		return 0, 0, err
	}

	if result.Queued == 0 && result.Skipped == len(contactIDs) {
		return 0, result.Skipped, fmt.Errorf("all contacts are suppressed")
	}

	if err := model.MarkCampaignSending(campaignID); err != nil {
		return result.Queued, result.Skipped, err
	}
	return result.Queued, result.Skipped, nil
}

func launchCampaign(userID, campaignID int64) (queued, skipped int, err error) {
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("campaign not found")
	}
	if campaign.Status == "sent" {
		return 0, 0, fmt.Errorf("campaign already sent")
	}
	if campaign.ExecutionMode == "workflow" && campaign.WorkflowVersionID > 0 {
		return startWorkflowCampaign(campaignID, campaign)
	}
	return executeCampaignSend(userID, campaignID)
}

func startWorkflowCampaign(campaignID int64, campaign model.Campaign) (sent, failed int, err error) {
	contactIDs, err := model.GetCampaignContactIDs(campaignID)
	if err != nil || len(contactIDs) == 0 {
		return 0, 0, fmt.Errorf("no contacts in campaign")
	}

	entry, err := model.GetEntryNodeKey(campaign.WorkflowVersionID)
	if err != nil {
		return 0, 0, fmt.Errorf("workflow has no entry node")
	}

	hasB := campaign.TemplateBID > 0
	for i, cid := range contactIDs {
		instID, err := model.CreateWorkflowInstance(campaign.WorkflowVersionID, cid, campaignID, entry)
		if err != nil {
			failed++
			continue
		}
		inst, _ := model.GetWorkflowInstance(instID)
		ctxMap := model.GetInstanceContext(&inst)
		variant := "A"
		if hasB && i%2 == 1 {
			variant = "B"
		}
		ctxMap["variant"] = variant
		_ = model.SetInstanceContext(&inst, ctxMap)
		_ = model.UpdateInstanceState(inst)

		v, _ := model.GetWorkflowVersion(campaign.WorkflowVersionID)
		_, _ = model.InsertContactEvent(model.ContactEventInput{
			ContactID:          cid,
			CampaignID:         campaignID,
			WorkflowID:         v.WorkflowID,
			WorkflowInstanceID: instID,
			EventType:          "WORKFLOW_STARTED",
		})

		if eng := workflow.GetEngine(); eng != nil {
			if ok, _ := model.ClaimInstance(instID); ok {
				_ = eng.ProcessInstance(instID)
			}
		}
		sent++
	}

	if err := model.MarkCampaignSent(campaignID); err != nil {
		return sent, failed, err
	}
	return sent, failed, nil
}

func parseScheduledAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	layouts := []string{"2006-01-02T15:04:05", "2006-01-02T15:04"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid format")
}

func DownloadCampaignSample(ctx *gin.Context) {
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "Invalid campaign ID")
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, mustUserID(ctx))
	if err != nil {
		ctx.String(http.StatusNotFound, "Campaign not found")
		return
	}
	vars, _ := model.MergeTemplateVariables(mustUserID(ctx), []int64{campaign.TemplateAID, campaign.TemplateBID})
	data, err := util.CreateContactSampleExcel(vars)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "Could not create sample")
		return
	}
	ctx.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	ctx.Header("Content-Disposition", "attachment; filename=campaign_contacts_sample.xlsx")
	ctx.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func parseContactIDs(ctx *gin.Context) []int64 {
	raw := ctx.PostFormArray("contact_ids")
	var ids []int64
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}