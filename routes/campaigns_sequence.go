package routes

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/sequence"
	"github.com/gin-gonic/gin"
)

type sequenceStepPayload struct {
	TemplateID         int64 `json:"template_id"`
	DelaySecondsBefore int   `json:"delay_seconds_before"`
	DelayDaysBefore    int   `json:"delay_days_before"`
}

// SaveCampaignSequenceWeb saves the Manage sequence steps.
func SaveCampaignSequenceWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}
	if !model.CampaignDraftMutable(campaign) {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape("Campaign cannot be edited now"))
		return
	}

	// Form: step_template[] + step_delay_days[]
	templates := ctx.PostFormArray("step_template")
	delays := ctx.PostFormArray("step_delay_days")
	var steps []model.CampaignSequenceStep
	for i := range templates {
		tid, _ := strconv.ParseInt(strings.TrimSpace(templates[i]), 10, 64)
		delayDays := 0
		if i < len(delays) {
			delayDays, _ = strconv.Atoi(strings.TrimSpace(delays[i]))
		}
		if delayDays < 0 {
			delayDays = 0
		}
		// First step always uses campaign A/B (template_id 0).
		if i == 0 {
			tid = 0
			delayDays = 0
		} else if tid > 0 && campaign.LibraryFolderID > 0 && !model.TemplateInFolder(userID, tid, campaign.LibraryFolderID) {
			ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape("Follow-up templates must be from this campaign folder"))
			return
		}
		steps = append(steps, model.CampaignSequenceStep{
			Position:           i + 1,
			TemplateID:         tid,
			DelaySecondsBefore: delayDays * 86400,
		})
	}
	if len(steps) == 0 {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape("Add at least one email step"))
		return
	}
	if err := model.SaveCampaignSequenceSteps(campaignID, steps); err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	// Ensure mode is sequence if saving steps.
	mode := campaign.ExecutionMode
	if !model.IsSequenceMode(mode) {
		mode = "sequence"
		if campaign.TemplateBID > 0 {
			mode = "sequence_ab"
		}
		_ = model.SetCampaignMode(userID, campaignID, mode)
	}
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape("Sequence saved"))
}

// ApplyRecommendedSequenceWeb inserts the recommended multi-touch preset.
func ApplyRecommendedSequenceWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}
	if !model.CampaignDraftMutable(campaign) {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape("Campaign cannot be edited now"))
		return
	}
	var tids []int64
	if campaign.LibraryFolderID > 0 {
		tids = model.ListLibraryFolderTemplateIDs(userID, campaign.LibraryFolderID)
	}
	if len(tids) == 0 && campaign.TemplateAID > 0 {
		tids = []int64{campaign.TemplateAID}
		if campaign.TemplateBID > 0 {
			tids = append(tids, campaign.TemplateBID)
		}
	}
	if err := model.ApplyRecommendedSequence(campaignID, tids); err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	mode := "sequence"
	if campaign.TemplateBID > 0 {
		mode = "sequence_ab"
	}
	_ = model.SetCampaignMode(userID, campaignID, mode)
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape("Recommended sequence applied"))
}

// SaveCampaignReplyWatchersWeb attaches published reply-watcher workflows.
// Send templates come from the workflow graph (set in the workflow editor), not from Manage.
func SaveCampaignReplyWatchersWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaignID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}
	allowed, _ := model.ListFolderReplyWorkflows(userID, campaign.LibraryFolderID)
	allowedByID := map[int64]model.CampaignWatcherWorkflow{}
	for _, w := range allowed {
		allowedByID[w.WorkflowID] = w
	}
	var workflowIDs []int64
	for _, raw := range ctx.PostFormArray("watcher_workflow") {
		wid, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if wid > 0 && allowedByID[wid].WorkflowID > 0 {
			workflowIDs = append(workflowIDs, wid)
		}
	}
	if err := model.SaveCampaignWatcherWorkflows(campaignID, workflowIDs); err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	for _, wid := range workflowIDs {
		w := allowedByID[wid]
		if w.VersionID > 0 {
			_ = model.SeedCampaignTemplatesFromWorkflowVersion(campaignID, w.VersionID)
		}
	}
	ctx.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape("Reply watchers saved"))
}

func startSequenceCampaign(campaignID int64, campaign model.Campaign) (sent, failed int, err error) {
	if err := model.ValidateCampaignSequenceReady(campaign); err != nil {
		return 0, 0, err
	}
	contactIDs, err := model.GetCampaignContactIDs(campaignID)
	if err != nil || len(contactIDs) == 0 {
		return 0, 0, fmt.Errorf("no contacts in campaign")
	}
	hasAB := campaign.ExecutionMode == "sequence_ab" && campaign.TemplateBID > 0

	outbound.BeginBulkEnqueue()
	defer outbound.EndBulkEnqueue()

	for i, cid := range contactIDs {
		variant := "A"
		if hasAB && i%2 == 1 {
			variant = "B"
		}
		_, created, err := model.CreateSequenceInstance(campaignID, cid, variant)
		if err != nil {
			failed++
			continue
		}
		if !created {
			continue
		}
		_, _ = model.InsertContactEvent(model.ContactEventInput{
			ContactID:  cid,
			CampaignID: campaignID,
			EventType:  "SEQUENCE_STARTED",
		})
		sent++
		if sent%25 == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if err := model.MarkCampaignSent(campaignID); err != nil {
		return sent, failed, err
	}
	go sequence.ProcessDueInstances()
	return sent, failed, nil
}
