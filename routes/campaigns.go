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

type WorkflowPickerItem struct {
	ID        int64
	Name      string
	VersionID int64
	StepCount int
}

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
	var workflowOptions []WorkflowPickerItem
	for _, w := range workflows {
		workflowOptions = append(workflowOptions, WorkflowPickerItem{
			ID:        w.ID,
			Name:      w.Name,
			VersionID: w.CurrentVersionID,
			StepCount: model.CountWorkflowSteps(w.CurrentVersionID),
		})
	}
	ctx.HTML(http.StatusOK, "campaigns_form.html", gin.H{
		"title":           "New Campaign",
		"active":          "campaigns",
		"templates":       templates,
		"workflows":       workflowOptions,
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
	experimentVariable := strings.TrimSpace(ctx.PostForm("experiment_variable"))
	experimentHypothesis := strings.TrimSpace(ctx.PostForm("experiment_hypothesis"))
	if executionMode == "workflow" {
		templateAID = 1 // placeholder for NOT NULL constraint; unused in workflow mode
	}

	id, err := model.CreateCampaign(userID, name, templateAID, templateBID, executionMode, workflowVersionID, experimentVariable, experimentHypothesis)
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
	contactLists, _ := model.ListContactLists(userID)

	isWorkflow := campaign.ExecutionMode == "workflow" && campaign.WorkflowVersionID > 0

	var scheduledAtLocal string
	if detail.ScheduledAt != nil {
		scheduledAtLocal = detail.ScheduledAt.Format("2006-01-02T15:04")
	}

	pageData := gin.H{
		"title":            detail.Name,
		"active":           "campaigns",
		"campaign":         detail,
		"allContacts":      allContacts,
		"contactLists":     contactLists,
		"linkedListID":     campaign.ContactListID,
		"scheduledAtLocal": scheduledAtLocal,
		"success":          ctx.Query("success"),
		"error":            ctx.Query("error"),
		"isWorkflow":       isWorkflow,
	}

	if isWorkflow {
		wfInfo, _ := model.GetWorkflowForVersion(campaign.WorkflowVersionID)
		wfSteps, _ := model.GetCampaignWorkflowStepDisplay(campaign.WorkflowVersionID)
		pageData["workflowInfo"] = wfInfo
		pageData["workflowSteps"] = wfSteps
		pageData["workflowContactRows"] = buildWorkflowCampaignContactRows(campaign, userID, contactIDs)
	} else {
		mergedVars, _ := model.MergeTemplateVariables(userID, []int64{detail.TemplateAID, detail.TemplateBID})
		templateA := templatePreviewView(userID, detail.TemplateAID)
		var templateB TemplatePreviewView
		hasB := detail.TemplateBID > 0
		if hasB {
			templateB = templatePreviewView(userID, detail.TemplateBID)
		}
		pageData["variables"] = strings.Join(mergedVars, ",")
		pageData["templateA"] = templateA
		pageData["templateB"] = templateB
		pageData["hasB"] = hasB
		pageData["contactRows"] = buildCampaignContactRows(campaign, userID, contactIDs, detail.TemplateAName, detail.TemplateBName)
		pageData["mergedVars"] = mergedVars
	}

	ctx.HTML(http.StatusOK, "campaigns_detail.html", pageData)
}

func CampaignAnalyticsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Invalid campaign ID"})
		return
	}

	campaign, err := model.GetCampaignForUser(id, userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Campaign not found"})
		return
	}

	if campaign.ExecutionMode == "workflow" && campaign.WorkflowVersionID > 0 {
		analytics, err := model.GetCampaignWorkflowAnalytics(id, userID)
		if err != nil {
			log.Print(err)
			ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "campaigns", "error": "Campaign not found"})
			return
		}
		ctx.HTML(http.StatusOK, "campaigns_workflow_analytics.html", gin.H{
			"title":     analytics.CampaignName + " Analytics",
			"active":    "campaigns",
			"analytics": analytics,
		})
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
		_, cid, err := model.UpsertContact(mustUserID(ctx), row.Email, cvs)
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
		_, cid, err := model.UpsertContact(mustUserID(ctx), row.Email, cvs)
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

	result, err := launchCampaign(mustUserID(ctx), campaignID)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}

	msg := fmt.Sprintf("Campaign queued: %d emails", result.Queued)
	if result.Skipped > 0 {
		if b := model.FormatSkipBreakdown(result.SkippedReasons); b != "" {
			msg += fmt.Sprintf(", %d skipped (%s)", result.Skipped, b)
		} else {
			msg += fmt.Sprintf(", %d skipped", result.Skipped)
		}
	}
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

func executeCampaignSend(userID, campaignID int64) (outbound.EnqueueResult, error) {
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		return outbound.EnqueueResult{}, fmt.Errorf("campaign not found")
	}
	if campaign.Status == "sent" {
		return outbound.EnqueueResult{}, fmt.Errorf("campaign already sent")
	}
	if campaign.IsSending {
		return outbound.EnqueueResult{}, fmt.Errorf("campaign is already sending")
	}

	contactIDs, err := model.GetCampaignContactIDs(campaignID)
	if err != nil || len(contactIDs) == 0 {
		return outbound.EnqueueResult{}, fmt.Errorf("no contacts in campaign")
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
		return outbound.EnqueueResult{}, err
	}

	if result.Queued == 0 && result.Skipped >= len(contactIDs) {
		b := model.FormatSkipBreakdown(result.SkippedReasons)
		if b != "" {
			return result, fmt.Errorf("all contacts skipped (%s)", b)
		}
		return result, fmt.Errorf("all contacts skipped (%d contacts)", len(contactIDs))
	}

	if err := model.MarkCampaignSending(campaignID); err != nil {
		return result, err
	}
	return result, nil
}

func launchCampaign(userID, campaignID int64) (outbound.EnqueueResult, error) {
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		return outbound.EnqueueResult{}, fmt.Errorf("campaign not found")
	}
	if campaign.Status == "sent" {
		return outbound.EnqueueResult{}, fmt.Errorf("campaign already sent")
	}
	if campaign.ExecutionMode == "workflow" && campaign.WorkflowVersionID > 0 {
		sent, failed, err := startWorkflowCampaign(campaignID, campaign)
		return outbound.EnqueueResult{Queued: sent, Skipped: failed}, err
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

func PromoteCampaignWinner(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}

	campaign, err := model.GetCampaignForUser(id, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}
	if campaign.TemplateBID <= 0 {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(id, 10)+"/analytics?error=No+clear+winner+yet")
		return
	}

	winner, _ := model.CampaignABWinner(id)
	var templateID int64
	switch winner {
	case "A":
		templateID = campaign.TemplateAID
	case "B":
		templateID = campaign.TemplateBID
	default:
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(id, 10)+"/analytics?error=No+clear+winner+yet")
		return
	}

	tpl, err := model.GetTemplateForUser(templateID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(id, 10)+"/analytics?error=Failed+to+promote+winner")
		return
	}

	newName := tpl.Name + " (winner)"
	newID, err := model.DuplicateTemplate(userID, templateID, newName)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(id, 10)+"/analytics?error=Failed+to+promote+winner")
		return
	}
	ctx.Redirect(http.StatusFound, "/templates/"+strconv.FormatInt(newID, 10)+"/edit?success=Winner+saved+as+new+template")
}