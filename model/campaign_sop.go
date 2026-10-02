package model

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	reHTMLTag    = regexp.MustCompile(`(?is)<[^>]+>`)
	reMultiSpace = regexp.MustCompile(`[ \t\xA0]+`)
	reMultiNL    = regexp.MustCompile(`\n{3,}`)
	reSOPSlug    = regexp.MustCompile(`[^a-z0-9]+`)
)

// BuildCampaignSOPMarkdown returns a human-readable SOP for one campaign.
func BuildCampaignSOPMarkdown(campaignID, userID int64) (content string, filename string, err error) {
	c, err := GetCampaignForUser(campaignID, userID)
	if err != nil {
		return "", "", err
	}

	var b strings.Builder
	writeln := func(format string, args ...interface{}) {
		fmt.Fprintf(&b, format+"\n", args...)
	}

	writeln("# Campaign SOP: %s", c.Name)
	writeln("")
	writeln("_Generated %s · Campaign ID %d_", time.Now().UTC().Format("2006-01-02 15:04 UTC"), c.ID)
	writeln("")

	writeln("## 1. Overview")
	writeln("")
	writeln("| Field | Value |")
	writeln("| --- | --- |")
	writeln("| Status | %s |", ComputeDisplayStatus(c.Status, c.ScheduledAt, c.IsSending))
	writeln("| Mode | %s |", sopExecutionModeLabel(c.ExecutionMode))
	writeln("| Created | %s |", c.CreatedAt.Format("2006-01-02 15:04"))
	if c.ScheduledAt != nil {
		writeln("| Scheduled | %s |", c.ScheduledAt.Format("2006-01-02 15:04"))
	}
	contactIDs, _ := GetCampaignContactIDs(c.ID)
	contactCount := len(contactIDs)
	writeln("| Contacts enrolled | %d |", contactCount)
	success := strings.TrimSpace(c.SuccessMetric)
	if success == "" {
		success = "reply"
	}
	writeln("| Success metric | %s |", success)
	writeln("| Stop on reply | %v |", c.StopOnReply)
	writeln("| Stop on hot lead | %v |", c.StopOnHot)
	writeln("| Open tracking | %v |", c.OpenTrackingEnabled)
	writeln("| Click tracking | %v |", c.ClickTrackingEnabled)
	writeln("")

	writeln("## 2. Strategy & experiment")
	writeln("")
	if strings.TrimSpace(c.ExperimentHypothesis) != "" || strings.TrimSpace(c.ExperimentVariable) != "" {
		if strings.TrimSpace(c.ExperimentHypothesis) != "" {
			writeln("**Hypothesis:** %s", strings.TrimSpace(c.ExperimentHypothesis))
			writeln("")
		}
		if strings.TrimSpace(c.ExperimentVariable) != "" {
			writeln("**Variable under test:** %s", strings.TrimSpace(c.ExperimentVariable))
			writeln("")
		}
	} else {
		writeln("No experiment hypothesis recorded for this campaign.")
		writeln("")
	}

	rules := ParseLeadTemperatureRulesJSON(c.TemperatureRulesJSON)
	writeln("### Lead temperature & stop rules")
	writeln("")
	writeln("%s", PreviewLeadTemperatureRules(rules))
	writeln("")

	writeln("## 3. Audience")
	writeln("")
	if c.ContactListID > 0 {
		if list, err := GetContactListForUser(c.ContactListID, userID); err == nil {
			writeln("- **Contact list:** %s (list ID %d)", list.Name, list.ID)
		} else {
			writeln("- **Contact list ID:** %d", c.ContactListID)
		}
	} else {
		writeln("- Contacts added directly to the campaign (no linked list), or list not set.")
	}
	writeln("- **Enrolled contacts:** %d", contactCount)
	writeln("")

	writeln("## 4. Sending mailboxes")
	writeln("")
	if sel, err := GetCampaignSMTPSelection(userID, c.ID); err == nil {
		if sel.AllowlistEmpty {
			writeln("All ready seats may send (no campaign-specific mailbox allowlist).")
			writeln("")
		}
		any := false
		for _, opt := range sel.Options {
			if !opt.Selected {
				continue
			}
			any = true
			ready := "ready"
			if !opt.Ready {
				ready = "not ready"
			}
			name := opt.Account.FromName
			email := opt.Account.SenderEmail()
			if name != "" {
				writeln("- %s · %s (%s)", name, email, ready)
			} else {
				writeln("- %s (%s)", email, ready)
			}
		}
		if !any {
			writeln("_No active mailboxes selected._")
		}
	} else {
		writeln("_Could not load mailbox selection._")
	}
	writeln("")

	templates := map[int64]Template{}
	collectTemplate := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := templates[id]; ok {
			return
		}
		if t, _, err := GetTemplateByID(id, userID); err == nil {
			templates[id] = t
		}
	}

	writeln("## 5. Sequence / workflow")
	writeln("")
	switch c.ExecutionMode {
	case "workflow", "workflow_ab":
		if c.WorkflowVersionID <= 0 {
			writeln("_Workflow version not set._")
			writeln("")
			break
		}
		if info, err := GetWorkflowForVersion(c.WorkflowVersionID); err == nil {
			writeln("**Workflow:** %s (version ID %d)", info.WorkflowName, c.WorkflowVersionID)
			writeln("")
		}
		if c.ExecutionMode == "workflow_ab" {
			writeln("First send step runs an **A/B test** using the campaign’s Variant A / Variant B templates.")
			writeln("")
		}
		steps, err := GetCampaignWorkflowStepDisplayForCampaign(c, userID)
		if err != nil {
			writeln("_Could not load workflow steps: %v_", err)
			writeln("")
			break
		}
		graph, _ := GetWorkflowGraph(c.WorkflowVersionID)
		nodeByKey := map[string]WorkflowNode{}
		for _, n := range graph.Nodes {
			nodeByKey[n.NodeKey] = n
		}
		for _, step := range steps {
			writeln("### Step %d — %s", step.StepIndex, step.Label)
			writeln("")
			writeln("- **Type:** `%s`", step.NodeType)
			writeln("- **Node key:** `%s`", step.NodeKey)
			if step.Description != "" {
				writeln("- **What it does:** %s", step.Description)
			}
			if step.IsHybridAB {
				writeln("- **A/B templates:** Variant A + Variant B (see Templates section)")
				collectTemplate(c.TemplateAID)
				collectTemplate(c.TemplateBID)
			} else if step.TemplateID > 0 {
				writeln("- **Template:** %s (ID %d)", step.TemplateName, step.TemplateID)
				collectTemplate(step.TemplateID)
			}
			var outs []string
			for _, e := range graph.Edges {
				if e.SourceNodeKey != step.NodeKey {
					continue
				}
				targetLabel := e.TargetNodeKey
				if tn, ok := nodeByKey[e.TargetNodeKey]; ok && tn.Label != "" {
					targetLabel = tn.Label
				}
				edge := e.EdgeType
				if edge == "" {
					edge = "default"
				}
				outs = append(outs, fmt.Sprintf("%s → %s", edge, targetLabel))
			}
			if len(outs) > 0 {
				writeln("- **Next:** %s", strings.Join(outs, "; "))
			}
			writeln("")
		}
	default:
		writeln("One-time (bulk) send — no multi-step workflow.")
		writeln("")
		if c.TemplateAID > 0 || c.TemplateBID > 0 {
			writeln("### Email variants")
			writeln("")
			if c.TemplateAID > 0 {
				collectTemplate(c.TemplateAID)
				name := "Variant A"
				if t, ok := templates[c.TemplateAID]; ok {
					name = t.Name
				}
				writeln("- **Variant A:** %s (template ID %d)", name, c.TemplateAID)
			}
			if c.TemplateBID > 0 {
				collectTemplate(c.TemplateBID)
				name := "Variant B"
				if t, ok := templates[c.TemplateBID]; ok {
					name = t.Name
				}
				writeln("- **Variant B:** %s (template ID %d) — contacts split A/B", name, c.TemplateBID)
			}
			writeln("")
		}
	}

	collectTemplate(c.TemplateAID)
	collectTemplate(c.TemplateBID)

	writeln("## 6. Email templates (full copy)")
	writeln("")
	if len(templates) == 0 {
		writeln("_No templates linked to this campaign._")
		writeln("")
	} else {
		orderedIDs := make([]int64, 0, len(templates))
		seen := map[int64]bool{}
		for _, id := range []int64{c.TemplateAID, c.TemplateBID} {
			if id > 0 && templates[id].ID > 0 && !seen[id] {
				orderedIDs = append(orderedIDs, id)
				seen[id] = true
			}
		}
		var rest []int64
		for id := range templates {
			if !seen[id] {
				rest = append(rest, id)
			}
		}
		sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
		orderedIDs = append(orderedIDs, rest...)

		for _, id := range orderedIDs {
			t := templates[id]
			role := ""
			if id == c.TemplateAID {
				role = " (Variant A)"
			} else if id == c.TemplateBID {
				role = " (Variant B)"
			}
			writeln("### %s%s", t.Name, role)
			writeln("")
			writeln("- **Template ID:** %d", t.ID)
			writeln("- **Subject:** %s", t.Subject)
			writeln("")
			writeln("**Body (plain text):**")
			writeln("")
			writeln("```")
			writeln("%s", sopPlainBody(t.Body))
			writeln("```")
			writeln("")
			writeln("<details><summary>Raw HTML body</summary>")
			writeln("")
			writeln("```html")
			writeln("%s", strings.TrimSpace(t.Body))
			writeln("```")
			writeln("")
			writeln("</details>")
			writeln("")
		}
	}

	writeln("## 7. How to operate this campaign")
	writeln("")
	writeln("1. Confirm audience and suppressions are clean (verify emails; keep bounce rate low).")
	writeln("2. Confirm sending mailboxes are ready and within warmup caps.")
	writeln("3. Review each template subject/body above before launch.")
	if c.ExecutionMode == "workflow" || c.ExecutionMode == "workflow_ab" {
		writeln("4. Walk the workflow steps: waits must fully elapse between follow-ups.")
		writeln("5. Start the workflow from the campaign page when ready; monitor analytics and mailbox health.")
	} else {
		writeln("4. Send the campaign when ready; monitor replies, bounces, and opt-outs.")
	}
	writeln("6. Pause or stop if bounce rate climbs or deliverability worsens.")
	writeln("")
	writeln("---")
	writeln("_End of SOP._")

	return b.String(), sopFilename(c), nil
}

func sopExecutionModeLabel(mode string) string {
	switch mode {
	case "workflow":
		return "Workflow sequence"
	case "workflow_ab":
		return "Hybrid workflow + A/B first email"
	case "bulk":
		return "One-time / bulk send"
	default:
		if mode == "" {
			return "One-time / bulk send"
		}
		return mode
	}
}

func sopFilename(c Campaign) string {
	name := strings.ToLower(strings.TrimSpace(c.Name))
	name = reSOPSlug.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "campaign"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return fmt.Sprintf("campaign-%d-%s-sop.md", c.ID, name)
}

func sopPlainBody(htmlBody string) string {
	s := htmlBody
	s = regexp.MustCompile(`(?is)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?is)</p>`).ReplaceAllString(s, "\n\n")
	s = regexp.MustCompile(`(?is)</div>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?is)</li>`).ReplaceAllString(s, "\n")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reMultiSpace.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	s = strings.Join(lines, "\n")
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return "(empty body)"
	}
	const max = 12000
	if len(s) > max {
		return s[:max] + "\n…(truncated)"
	}
	return s
}
