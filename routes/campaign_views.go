package routes

import (
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
}

type WorkflowCampaignContactRowView struct {
	Index          int
	ID             int64
	Email          string
	NodeKey        string
	InstanceStatus string
	CurrentStep    string
	OpenCount      int
	ClickCount     int
	HasStarted     bool
	SendID         int64
}

func buildWorkflowCampaignContactRows(
	campaign model.Campaign,
	userID int64,
	contactIDs []int64,
) []WorkflowCampaignContactRowView {
	instanceMap, _ := model.GetCampaignInstanceMap(campaign.ID)
	sendMap := map[int64]model.ContactEngagementRow{}
	analytics, _ := model.GetCampaignAnalytics(campaign.ID, userID)
	for _, c := range analytics.Contacts {
		sendMap[c.ContactID] = c
	}

	var rows []WorkflowCampaignContactRowView
	for i, cid := range contactIDs {
		contact, _, err := model.GetContact(cid)
		if err != nil {
			continue
		}
		row := WorkflowCampaignContactRowView{
			Index: i + 1,
			ID:    contact.ID,
			Email: contact.Email,
		}
		if inst, ok := instanceMap[cid]; ok {
			row.HasStarted = true
			row.InstanceStatus = inst.Status
			row.NodeKey = inst.CurrentNodeKey
			row.CurrentStep = model.NodeLabelForKey(campaign.WorkflowVersionID, inst.CurrentNodeKey)
		} else {
			row.InstanceStatus = "not started"
			row.CurrentStep = "—"
		}
		if sent, ok := sendMap[cid]; ok {
			row.OpenCount = sent.OpenCount
			row.ClickCount = sent.ClickCount
			if sent.SendID > 0 {
				row.SendID = sent.SendID
				row.HasStarted = true
			}
		}
		rows = append(rows, row)
	}
	return rows
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

	sendMap := map[int64]model.ContactEngagementRow{}
	analytics, _ := model.GetCampaignAnalytics(campaign.ID, userID)
	for _, c := range analytics.Contacts {
		sendMap[c.ContactID] = c
	}

	var rows []CampaignContactRowView
	for i, cid := range contactIDs {
		contact, vars, err := model.GetContact(cid)
		if err != nil {
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

		varMap := make(map[string]string)
		for _, v := range vars {
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

		subject, _ := util.RenderTemplate(tpl.Subject, vars, "")
		body, _ := util.RenderTemplate(tpl.Body, vars, "")
		body = truncatePreview(body, 500)

		row := CampaignContactRowView{
			Index:           i + 1,
			ID:              contact.ID,
			Email:           contact.Email,
			Variant:         variant,
			TemplateName:    templateName,
			TemplateID:      templateID,
			Variables:       cells,
			MissingVars:     missing,
			RenderedSubject: subject,
			RenderedBody:    body,
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
