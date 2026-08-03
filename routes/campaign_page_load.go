package routes

import (
	"context"
	"strings"

	"emailtracker.com/model"
	"golang.org/x/sync/errgroup"
)

type campaignDetailPageData struct {
	ContactIDs          []int64
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

func loadCampaignDetailPageData(userID int64, detail model.CampaignDetail, pickerFilter model.ContactListFilter) (campaignDetailPageData, error) {
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

	g, _ := errgroup.WithContext(context.Background())

	g.Go(func() error {
		var err error
		data.ContactIDs, err = model.GetCampaignContactIDs(detail.ID)
		return err
	})
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

	g2, _ := errgroup.WithContext(context.Background())
	if isWorkflow {
		g2.Go(func() error {
			data.WorkflowContactRows = buildWorkflowCampaignContactRows(campaign, userID, data.ContactIDs)
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
	} else {
		g2.Go(func() error {
			data.ContactRows = buildCampaignContactRows(campaign, userID, data.ContactIDs, detail.TemplateAName, detail.TemplateBName)
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
	}
	_ = g2.Wait()
	return data, nil
}

func campaignDetailVariablesString(vars []string) string {
	return strings.Join(vars, ",")
}
