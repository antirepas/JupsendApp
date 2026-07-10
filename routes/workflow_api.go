package routes

import (
	"log"
	"net/http"
	"strconv"

	"emailtracker.com/model"
	"emailtracker.com/workflow"
	"github.com/gin-gonic/gin"
)

func RegisterWorkflowAPI(v1 *gin.RouterGroup) {
	v1.GET("/workflows/:id/versions/:vid/send-steps", apiListSendSteps)
	v1.PUT("/campaigns/:id/workflow-templates", apiSaveCampaignWorkflowTemplates)
	v1.GET("/workflows", apiListWorkflows)
	v1.POST("/workflows", apiCreateWorkflow)
	v1.GET("/workflows/:id", apiGetWorkflow)
	v1.GET("/workflows/:id/versions/:vid", apiGetWorkflowGraph)
	v1.PUT("/workflows/:id/versions/:vid", apiSaveWorkflowGraph)
	v1.POST("/workflows/:id/versions/:vid/publish", apiPublishWorkflow)
	v1.POST("/workflows/:id/versions/:vid/validate", apiValidateWorkflow)
	v1.GET("/workflow-instances/:id", apiGetInstance)
	v1.POST("/workflow-instances/:id/cancel", apiCancelInstance)
	v1.POST("/events", apiIngestEvent)
	v1.GET("/contacts/:id/events", apiContactEvents)
	v1.GET("/workflows/:id/analytics", apiWorkflowAnalytics)
}

func apiListSendSteps(ctx *gin.Context) {
	wid, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	vid, _ := strconv.ParseInt(ctx.Param("vid"), 10, 64)
	if _, err := model.GetWorkflowForUser(wid, mustUserID(ctx)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	v, err := model.GetWorkflowVersion(vid)
	if err != nil || v.WorkflowID != wid {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	steps, err := model.ListSendEmailSteps(vid)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	firstKey, _ := model.GetFirstSendNodeKey(vid)
	ctx.JSON(http.StatusOK, gin.H{"send_steps": steps, "first_send_node_key": firstKey})
}

func apiSaveCampaignWorkflowTemplates(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaignID, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if campaign.Status == "sent" || campaign.Status == "sending" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "cannot edit after launch"})
		return
	}
	var body struct {
		Templates   map[string]int64 `json:"templates"`
		TemplateAID int64            `json:"template_a_id"`
		TemplateBID int64            `json:"template_b_id"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if campaign.ExecutionMode == "workflow_ab" {
		if body.TemplateAID <= 0 || body.TemplateBID <= 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "template_a_id and template_b_id required for workflow_ab"})
			return
		}
		if err := model.UpdateCampaignTemplates(campaignID, userID, body.TemplateAID, body.TemplateBID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else if len(body.Templates) > 0 {
		firstKey, err := model.GetFirstSendNodeKey(campaign.WorkflowVersionID)
		if err == nil && body.Templates[firstKey] > 0 {
			_ = model.UpdateCampaignTemplates(campaignID, userID, body.Templates[firstKey], 0)
		}
	}
	if err := model.SaveCampaignWorkflowTemplates(campaignID, body.Templates); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := model.ValidateCampaignWorkflowReady(campaign); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"saved": true, "warning": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"saved": true})
}

func apiListWorkflows(ctx *gin.Context) {
	list, err := model.ListWorkflows(mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, list)
}

func apiCreateWorkflow(ctx *gin.Context) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	id, err := model.CreateWorkflow(mustUserID(ctx), body.Name, body.Description)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	w, _ := model.GetWorkflowForUser(id, mustUserID(ctx))
	ctx.JSON(http.StatusOK, w)
}

func apiGetWorkflow(ctx *gin.Context) {
	id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	w, err := model.GetWorkflowForUser(id, mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx.JSON(http.StatusOK, w)
}

func apiGetWorkflowGraph(ctx *gin.Context) {
	wid, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	vid, _ := strconv.ParseInt(ctx.Param("vid"), 10, 64)
	if _, err := model.GetWorkflowForUser(wid, mustUserID(ctx)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	v, err := model.GetWorkflowVersion(vid)
	if err != nil || v.WorkflowID != wid {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	g, err := model.GetWorkflowGraph(vid)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, g)
}

func workflowMustBeActive(ctx *gin.Context, workflowID int64) (model.Workflow, bool) {
	w, err := model.GetWorkflowForUser(workflowID, mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return w, false
	}
	if model.WorkflowIsArchived(w) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "workflow is archived"})
		return w, false
	}
	return w, true
}

func apiSaveWorkflowGraph(ctx *gin.Context) {
	wid, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	vid, _ := strconv.ParseInt(ctx.Param("vid"), 10, 64)
	if _, ok := workflowMustBeActive(ctx, wid); !ok {
		return
	}
	v, err := model.GetWorkflowVersion(vid)
	if err != nil || v.WorkflowID != wid {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var input model.GraphSaveInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := model.SaveWorkflowGraph(vid, input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "saved"})
}

func apiPublishWorkflow(ctx *gin.Context) {
	wid, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	vid, _ := strconv.ParseInt(ctx.Param("vid"), 10, 64)
	if _, ok := workflowMustBeActive(ctx, wid); !ok {
		return
	}
	if err := model.PublishWorkflowVersion(wid, vid); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "published"})
}

func apiValidateWorkflow(ctx *gin.Context) {
	wid, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	vid, _ := strconv.ParseInt(ctx.Param("vid"), 10, 64)
	if _, ok := workflowMustBeActive(ctx, wid); !ok {
		return
	}
	v, err := model.GetWorkflowVersion(vid)
	if err != nil || v.WorkflowID != wid {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	errs := model.ValidateWorkflowGraph(vid)
	if len(errs) > 0 {
		ctx.JSON(http.StatusOK, gin.H{"valid": false, "errors": errs})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"valid": true})
}

func apiGetInstance(ctx *gin.Context) {
	id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	inst, err := model.GetWorkflowInstance(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	execs, _ := model.GetExecutionsForInstance(id)
	ctx.JSON(http.StatusOK, gin.H{"instance": inst, "executions": execs})
}

func apiCancelInstance(ctx *gin.Context) {
	id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err := model.CancelInstance(id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "cancelled"})
}

func apiIngestEvent(ctx *gin.Context) {
	var body struct {
		ContactID   int64                  `json:"contact_id"`
		CampaignID  int64                  `json:"campaign_id"`
		EmailSendID int64                  `json:"email_send_id"`
		EventType   string                 `json:"event_type"`
		Metadata    map[string]interface{} `json:"metadata"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	id, err := model.InsertContactEvent(model.ContactEventInput{
		ContactID:   body.ContactID,
		CampaignID:  body.CampaignID,
		EmailSendID: body.EmailSendID,
		EventType:   body.EventType,
		Metadata:    body.Metadata,
	})
	if err != nil {
		log.Print(err)
	}
	workflow.DispatchContactEvent(body.ContactID, body.EventType)
	ctx.JSON(http.StatusOK, gin.H{"id": id})
}

func apiContactEvents(ctx *gin.Context) {
	id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	events, err := model.GetContactEvents(id, 100)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, events)
}

func apiWorkflowAnalytics(ctx *gin.Context) {
	id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
	w, err := model.GetWorkflowForUser(id, mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	stats, _ := model.CountInstancesByStatus(id)
	nodeStats, _ := model.GetWorkflowNodeStats(w.CurrentVersionID)
	ctx.JSON(http.StatusOK, gin.H{
		"status_counts": stats,
		"node_stats":    nodeStats,
	})
}
