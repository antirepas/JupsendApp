package routes

import (
	"fmt"
	"html"
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/util"
)

type TemplatePreviewView struct {
	ID                 int64
	Name               string
	Subject            string
	Body               string
	Variables          []string
	HighlightedSubject string
	HighlightedBody    string
}

type ContactVariableCell struct {
	Key     string
	Value   string
	Missing bool
}

type CampaignContactRowView struct {
	Index           int
	ID              int64
	Email           string
	Variant         string
	TemplateName    string
	TemplateID      int64
	Variables       []ContactVariableCell
	MissingVars     []string
	RenderedSubject string
	RenderedBody    string
	Sent            bool
	SendID          int64
	OpenCount       int
	ClickCount      int
	Replied         bool
}

type WorkflowCampaignContactRowView struct {
	Index          int
	ID             int64
	Email          string
	NodeKey        string
	InstanceStatus string
	CurrentStep    string
	BranchLabel    string
	OpenCount      int
	ClickCount     int
	HasStarted     bool
	SendID         int64
	Replied        bool
	Sent           bool
}

// WorkflowGraphNodeView carries page context into recursive wf_graph_node template calls.
type WorkflowGraphNodeView struct {
	NodeKey             string
	Label               string
	NodeType            string
	Description         string
	TemplateID          int64
	TemplateName        string
	IsHybridAB          bool
	IsForkBranch        bool
	EdgePriority        int
	EdgeType            string
	EdgeLabel           string
	CanEditMappings     bool
	Templates           []model.TemplateListItem
	CampaignTemplateAID int64
	CampaignTemplateBID int64
	StepMappings        map[string]int64
	Children            []WorkflowGraphNodeView
}

func wrapWorkflowGraphNode(
	n model.CampaignWorkflowGraphNode,
	canEdit bool,
	templates []model.TemplateListItem,
	mappings map[string]int64,
	templateAID, templateBID int64,
) WorkflowGraphNodeView {
	v := WorkflowGraphNodeView{
		NodeKey:             n.NodeKey,
		Label:               n.Label,
		NodeType:            n.NodeType,
		Description:         n.Description,
		TemplateID:          n.TemplateID,
		TemplateName:        n.TemplateName,
		IsHybridAB:          n.IsHybridAB,
		IsForkBranch:        n.IsForkBranch,
		EdgePriority:        n.EdgePriority,
		EdgeType:            n.EdgeType,
		EdgeLabel:           n.EdgeLabel,
		CanEditMappings:     canEdit,
		Templates:           templates,
		CampaignTemplateAID: templateAID,
		CampaignTemplateBID: templateBID,
		StepMappings:        mappings,
	}
	for _, child := range n.Children {
		v.Children = append(v.Children, wrapWorkflowGraphNode(child, canEdit, templates, mappings, templateAID, templateBID))
	}
	return v
}

func buildWorkflowCampaignContactRows(
	campaign model.Campaign,
	userID int64,
	contactIDs []int64,
) []WorkflowCampaignContactRowView {
	instances, _ := model.ListInstancesForCampaign(campaign.ID)
	sendMap, _ := model.GetCampaignContactEngagementLite(campaign.ID)
	emailMap, _ := model.GetCampaignContactEmailMap(campaign.ID)
	replied := model.CampaignRepliedContactSet(campaign.ID)
	labels := model.NodeLabelMapForVersion(campaign.WorkflowVersionID)

	if len(instances) > 0 {
		var rows []WorkflowCampaignContactRowView
		for i, inst := range instances {
			email := emailMap[inst.ContactID]
			row := WorkflowCampaignContactRowView{
				Index:          i + 1,
				ID:             inst.ContactID,
				Email:          email,
				HasStarted:     true,
				InstanceStatus: inst.Status,
				NodeKey:        inst.CurrentNodeKey,
				CurrentStep:    model.LabelFromMap(labels, inst.CurrentNodeKey),
				BranchLabel:    workflowBranchLabel(inst),
				Replied:        replied[inst.ContactID],
			}
			if sent, ok := sendMap[inst.ContactID]; ok {
				row.OpenCount = sent.OpenCount
				row.ClickCount = sent.ClickCount
				if sent.SendID > 0 {
					row.SendID = sent.SendID
					row.Sent = true
				}
			}
			rows = append(rows, row)
		}
		return rows
	}

	var rows []WorkflowCampaignContactRowView
	for i, cid := range contactIDs {
		row := WorkflowCampaignContactRowView{
			Index:   i + 1,
			ID:      cid,
			Email:   emailMap[cid],
			Replied: replied[cid],
		}
		row.InstanceStatus = "not started"
		row.CurrentStep = "—"
		if sent, ok := sendMap[cid]; ok {
			row.OpenCount = sent.OpenCount
			row.ClickCount = sent.ClickCount
			if sent.SendID > 0 {
				row.SendID = sent.SendID
				row.HasStarted = true
				row.Sent = true
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func workflowBranchLabel(inst model.WorkflowInstance) string {
	if inst.ForkRootID != nil && *inst.ForkRootID > 0 {
		return fmt.Sprintf("Branch (priority %d)", inst.BranchPriority)
	}
	if inst.BranchPriority > 0 {
		return fmt.Sprintf("Main (priority %d)", inst.BranchPriority)
	}
	return "Main"
}

func experimentVariableLabel(variable string) string {
	switch variable {
	case "subject":
		return "Subject line"
	case "opener":
		return "Opening line"
	case "offer":
		return "Offer / value prop"
	case "full_message":
		return "Full message"
	case "other":
		return "Other"
	default:
		return variable
	}
}

func buildCampaignContactRows(
	campaign model.Campaign,
	userID int64,
	contactIDs []int64,
	templateAName, templateBName string,
) []CampaignContactRowView {
	hasB := campaign.TemplateBID > 0
	templateA, aVars, _ := model.GetTemplateByID(campaign.TemplateAID, userID)
	templateB := model.Template{}
	var bVars []string
	if hasB {
		templateB, bVars, _ = model.GetTemplateByID(campaign.TemplateBID, userID)
	}

	sendMap, _ := model.GetCampaignContactEngagementLite(campaign.ID)
	contactData, _ := model.GetCampaignContactDataMap(campaign.ID)
	replied := model.CampaignRepliedContactSet(campaign.ID)

	var rows []CampaignContactRowView
	for i, cid := range contactIDs {
		data := contactData[cid]
		if data.Email == "" && len(data.Variables) == 0 {
			continue
		}

		variant := "A"
		templateName := templateAName
		templateID := campaign.TemplateAID
		tpl := templateA
		tplVars := aVars
		if hasB && i%2 == 1 {
			variant = "B"
			templateName = templateBName
			templateID = campaign.TemplateBID
			tpl = templateB
			tplVars = bVars
		}

		varMap := make(map[string]string, len(data.Variables))
		for _, v := range data.Variables {
			varMap[v.Key] = v.Value
		}

		var cells []ContactVariableCell
		var missing []string
		for _, key := range tplVars {
			val := varMap[key]
			missingVal := strings.TrimSpace(val) == ""
			if missingVal {
				missing = append(missing, key)
			}
			cells = append(cells, ContactVariableCell{Key: key, Value: val, Missing: missingVal})
		}

		subject, body, _, _ := util.RenderEmail(tpl.Subject, tpl.Body, data.Variables, util.RenderOptions{ForPreview: true, BodyMode: true})
		body = truncatePreview(body, 500)

		row := CampaignContactRowView{
			Index:           i + 1,
			ID:              cid,
			Email:           data.Email,
			Variant:         variant,
			TemplateName:    templateName,
			TemplateID:      templateID,
			Variables:       cells,
			MissingVars:     missing,
			RenderedSubject: subject,
			RenderedBody:    body,
			Replied:         replied[cid],
		}

		if sent, ok := sendMap[cid]; ok && sent.SendID > 0 {
			row.Sent = true
			row.SendID = sent.SendID
			row.OpenCount = sent.OpenCount
			row.ClickCount = sent.ClickCount
		}

		rows = append(rows, row)
	}
	return rows
}

// filterCampaignContactRows filters manage-page contact rows by email + campaign engagement.
// engagement: "", opened, clicked, replied, not_sent, sent, missing_vars
func filterCampaignContactRows(rows []CampaignContactRowView, q, engagement string) []CampaignContactRowView {
	q = strings.ToLower(strings.TrimSpace(q))
	engagement = strings.TrimSpace(engagement)
	out := make([]CampaignContactRowView, 0, len(rows))
	for _, row := range rows {
		if q != "" && !strings.Contains(strings.ToLower(row.Email), q) {
			continue
		}
		switch engagement {
		case "opened":
			if row.OpenCount <= 0 {
				continue
			}
		case "clicked":
			if row.ClickCount <= 0 {
				continue
			}
		case "replied":
			if !row.Replied {
				continue
			}
		case "not_sent":
			if row.Sent {
				continue
			}
		case "sent":
			if !row.Sent {
				continue
			}
		case "missing_vars":
			if len(row.MissingVars) == 0 {
				continue
			}
		}
		out = append(out, row)
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

func filterWorkflowCampaignContactRows(rows []WorkflowCampaignContactRowView, q, engagement string) []WorkflowCampaignContactRowView {
	q = strings.ToLower(strings.TrimSpace(q))
	engagement = strings.TrimSpace(engagement)
	out := make([]WorkflowCampaignContactRowView, 0, len(rows))
	for _, row := range rows {
		if q != "" && !strings.Contains(strings.ToLower(row.Email), q) {
			continue
		}
		switch engagement {
		case "opened":
			if row.OpenCount <= 0 {
				continue
			}
		case "clicked":
			if row.ClickCount <= 0 {
				continue
			}
		case "replied":
			if !row.Replied {
				continue
			}
		case "not_sent":
			if row.Sent {
				continue
			}
		case "sent":
			if !row.Sent {
				continue
			}
		}
		out = append(out, row)
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

func truncatePreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func templatePreviewView(userID, id int64) TemplatePreviewView {
	t, vars, err := model.GetTemplateByID(id, userID)
	if err != nil {
		return TemplatePreviewView{}
	}
	return TemplatePreviewView{
		ID:                 t.ID,
		Name:               t.Name,
		Subject:            t.Subject,
		Body:               t.Body,
		Variables:          vars,
		HighlightedSubject: highlightPlaceholders(t.Subject, vars),
		HighlightedBody:    highlightPlaceholders(t.Body, vars),
	}
}

func highlightPlaceholders(text string, vars []string) string {
	out := html.EscapeString(text)
	for _, v := range vars {
		placeholder := "{{" + v + "}}"
		out = strings.ReplaceAll(out, html.EscapeString(placeholder),
			"<mark class=\"bg-amber-100 text-amber-900 px-1 rounded\">"+html.EscapeString(placeholder)+"</mark>")
	}
	return out
}
