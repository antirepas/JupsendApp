package routes

import (
	"context"
	"strings"

	"emailtracker.com/model"
	"golang.org/x/sync/errgroup"
)

type campaignDetailPageData struct {
	PickerPage          model.ContactListPage
	ContactLists        []model.ContactList
	WorkflowInfo        model.WorkflowVersionInfo
	WorkflowGraphTree   model.CampaignWorkflowGraphNode
	StepMappings        map[string]int64
	FirstSendNodeKey    string
	Templates           []model.TemplateListItem
	WorkflowContactRows []WorkflowCampaignContactRowView
	TemplateA           TemplatePreviewView
	TemplateB           TemplatePreviewView
	HasB                bool
	MergedVars          []string
	ContactRows         []CampaignContactRowView
	MemberPage          memberListPage
	MemberTotal         int
	MissingVarsCount    int
	VarCoverage         []model.CampaignVarCoverage
	ReadyContactCount   int
}

type campaignMemberLoadOpts struct {
	Query      string
	Engagement string
	Page       int
	PageSize   int
}

func campaignFromDetail(d model.CampaignDetail) model.Campaign {
	return model.Campaign{
		ID:                   d.ID,
		Name:                 d.Name,
		TemplateAID:          d.TemplateAID,
		TemplateBID:          d.TemplateBID,
		Status:               d.Status,
		IsSending:            d.IsSending,
		ScheduledAt:          d.ScheduledAt,
		ExecutionMode:        d.ExecutionMode,
		WorkflowVersionID:    d.WorkflowVersionID,
		ContactListID:        d.ContactListID,
		ExperimentVariable:   d.ExperimentVariable,
		ExperimentHypothesis: d.ExperimentHypothesis,
	}
}

func loadCampaignDetailPageData(userID int64, detail model.CampaignDetail, pickerFilter model.ContactListFilter, memberOpts campaignMemberLoadOpts) (campaignDetailPageData, error) {
	var data campaignDetailPageData
	campaign := campaignFromDetail(detail)
	isWorkflow := (detail.ExecutionMode == "workflow" || detail.ExecutionMode == "workflow_ab") && detail.WorkflowVersionID > 0

	pickerFilter.ExcludeCampaignID = detail.ID
	pickerFilter.Lite = true
	if pickerFilter.PageSize < 1 {
		pickerFilter.PageSize = 50
	}
	if pickerFilter.Sort == "" {
		pickerFilter.Sort = "email"
	}
	if memberOpts.PageSize < 1 {
		memberOpts.PageSize = 50
	}
	if memberOpts.Page < 1 {
		memberOpts.Page = 1
	}

	g, _ := errgroup.WithContext(context.Background())

	// While launching, skip contact pickers — they add DB load and aren't usable mid-send.
	if !detail.IsSending {
		g.Go(func() error {
			var err error
			data.PickerPage, err = model.ListContactsFiltered(userID, pickerFilter)
			return err
		})
		g.Go(func() error {
			var err error
			data.ContactLists, err = model.ListContactLists(userID)
			return err
		})
	}

	if isWorkflow {
		g.Go(func() error {
			var err error
			data.WorkflowInfo, err = model.GetWorkflowForVersion(detail.WorkflowVersionID)
			return err
		})
		g.Go(func() error {
			var err error
			data.WorkflowGraphTree, err = model.BuildCampaignWorkflowGraphTree(campaign, userID)
			return err
		})
		g.Go(func() error {
			var err error
			data.StepMappings, err = model.GetCampaignWorkflowTemplates(detail.ID)
			return err
		})
		g.Go(func() error {
			var err error
			data.FirstSendNodeKey, err = model.GetFirstSendNodeKey(detail.WorkflowVersionID)
			return err
		})
		g.Go(func() error {
			var err error
			data.Templates, err = model.ListTemplatePickerItems(userID)
			return err
		})
	} else {
		g.Go(func() error {
			var err error
			data.MergedVars, err = model.MergeTemplateVariables(userID, []int64{detail.TemplateAID, detail.TemplateBID})
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return data, err
	}

	if isWorkflow {
		ids := []int64{detail.TemplateAID, detail.TemplateBID}
		for _, tid := range data.StepMappings {
			ids = append(ids, tid)
		}
		data.MergedVars, _ = model.MergeTemplateVariables(userID, ids)
	}

	memberFilter := model.CampaignMemberFilter{
		Query:      memberOpts.Query,
		Engagement: memberOpts.Engagement,
		Page:       memberOpts.Page,
		PageSize:   memberOpts.PageSize,
	}
	// Full-contact var scans compete with launch enqueue for the DB pool and are
	// what turn "send" into a 2-minute 502. Skip them while the campaign is launching.
	skipHeavyVarScans := detail.IsSending || model.HasActiveCampaignListImport(userID, detail.ID)

	if isWorkflow {
		memberFilter.MergedVars = data.MergedVars
		if memberOpts.Engagement != "missing_vars" && !skipHeavyVarScans {
			data.MissingVarsCount, _ = model.CountCampaignContactsMissingAnyVars(detail.ID, data.MergedVars)
		}
	} else {
		_, aVars, _ := model.GetTemplateByID(detail.TemplateAID, userID)
		var bVars []string
		hasB := detail.TemplateBID > 0
		if hasB {
			_, bVars, _ = model.GetTemplateByID(detail.TemplateBID, userID)
		}
		memberFilter.HasB = hasB
		memberFilter.TemplateAVars = aVars
		memberFilter.TemplateBVars = bVars
		if memberOpts.Engagement != "missing_vars" && !skipHeavyVarScans {
			data.MissingVarsCount, _ = model.CountCampaignContactsMissingVars(detail.ID, aVars, bVars, hasB)
		}
	}
	if len(data.MergedVars) > 0 && !skipHeavyVarScans {
		data.VarCoverage, _ = model.CampaignVariableCoverage(detail.ID, data.MergedVars)
		data.ReadyContactCount = detail.ContactCount - data.MissingVarsCount
		if data.ReadyContactCount < 0 {
			data.ReadyContactCount = 0
		}
	}

	memberPage, err := model.ListCampaignMemberPage(detail.ID, memberFilter)
	if err != nil {
		return data, err
	}
	data.MemberTotal = memberPage.Total
	data.MemberPage = buildMemberListPage(memberPage.Total, memberPage.Page, memberPage.PageSize)
	indexBase := (data.MemberPage.Page - 1) * data.MemberPage.PageSize

	g2, _ := errgroup.WithContext(context.Background())
	if isWorkflow {
		mergedVars := data.MergedVars
		g2.Go(func() error {
			data.WorkflowContactRows = buildWorkflowCampaignContactRows(campaign, userID, memberPage.ContactIDs, indexBase, mergedVars)
			return nil
		})
		if detail.ExecutionMode == "workflow_ab" {
			g2.Go(func() error {
				data.TemplateA = templatePreviewView(userID, detail.TemplateAID)
				return nil
			})
			g2.Go(func() error {
				data.HasB = detail.TemplateBID > 0
				if data.HasB {
					data.TemplateB = templatePreviewView(userID, detail.TemplateBID)
				}
				return nil
			})
		}
		if memberOpts.Engagement == "missing_vars" {
			data.MissingVarsCount = memberPage.Total
		}
	} else {
		g2.Go(func() error {
			idxMap, _ := model.GetCampaignContactIndexMap(detail.ID, memberPage.ContactIDs)
			data.ContactRows = buildCampaignContactRows(campaign, userID, memberPage.ContactIDs, idxMap, detail.TemplateAName, detail.TemplateBName)
			return nil
		})
		g2.Go(func() error {
			data.TemplateA = templatePreviewView(userID, detail.TemplateAID)
			return nil
		})
		g2.Go(func() error {
			data.HasB = detail.TemplateBID > 0
			if data.HasB {
				data.TemplateB = templatePreviewView(userID, detail.TemplateBID)
			}
			return nil
		})
		if memberOpts.Engagement == "missing_vars" {
			data.MissingVarsCount = memberPage.Total
		}
	}
	_ = g2.Wait()
	return data, nil
}

func campaignDetailVariablesString(vars []string) string {
	return strings.Join(vars, ",")
}
