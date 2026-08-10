package routes

import (
	"net/http"
	"strings"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func GuidesIndex(c *gin.Context) {
	c.HTML(http.StatusOK, "guides_index.html", gin.H{
		"title":  "Guides",
		"active": "guides",
	})
}

func GuideGettingStarted(c *gin.Context) {
	c.HTML(http.StatusOK, "guide_getting_started.html", gin.H{
		"title":     "Getting started",
		"pageTitle": "Getting started with jupsend",
		"subtitle":  "An optional playbook for running outbound outreach the right way — from mailbox to follow-ups.",
		"active":    "getting-started",
	})
}

func GuideGmail(c *gin.Context) {
	renderGuide(c, "guide_gmail.html", "Google sign-in", "Google sign-in", "Google is for signing in. Sending always goes through Mailboxes (shared Free seat or your Pro domain).")
}

func GuideContacts(c *gin.Context) {
	renderGuide(c, "guide_contacts.html", "Contacts", "Contacts playbook", "Import clean lists, map variables, and keep suppressions healthy so every campaign starts with good data.")
}

func GuideTemplates(c *gin.Context) {
	renderGuide(c, "guide_templates.html", "Templates", "Templates playbook", "Write personalized outreach that sounds human, uses variables correctly, and earns replies.")
}

func GuideCampaigns(c *gin.Context) {
	renderGuide(c, "guide_campaigns.html", "Campaigns", "Campaigns playbook", "Choose the right campaign type, launch safely, and use lead temperature + analytics to improve.")
}

func GuideWorkflows(c *gin.Context) {
	renderGuide(c, "guide_workflows.html", "Workflows", "Workflows playbook", "Design follow-up sequences with waits, engagement branches, and lead temperature so warm leads get the right email.")
}

func GuideOutreach(c *gin.Context) {
	c.HTML(http.StatusOK, "guide_outreach.html", gin.H{
		"title":     "Outreach playbook",
		"pageTitle": "Recommended outreach playbook",
		"subtitle":  "The jupsend default system: A/B cold → temperature → value prop or nudge → breakup → manual Loom close for hot/warm leads.",
		"active":    "outreach",
	})
}

func GuideMailboxes(c *gin.Context) {
	renderGuide(c, "guide_mailboxes.html", "Mailboxes", "Mailboxes playbook", "Set up Free shared sending or Pro domains and seats, then protect deliverability.")
}

func GuideSends(c *gin.Context) {
	renderGuide(c, "guide_sends.html", "Sends", "Sends playbook", "Read delivery status, human vs bot opens, and how to debug a single send.")
}

func GuideInterested(c *gin.Context) {
	renderGuide(c, "guide_interested.html", "Interested contacts", "Interested contacts playbook", "Prioritize people who replied, clicked, or opened — then follow up while interest is fresh.")
}

func GuideAnalytics(c *gin.Context) {
	renderGuide(c, "guide_analytics.html", "Analytics", "Analytics playbook", "Turn opens, clicks, and replies into decisions: what to double down on and what to stop.")
}

func GuideWizardDismiss(c *gin.Context) {
	userID := mustUserID(c)
	_ = model.SetWizardDismissed(userID)
	next := safeRelativePath(c.PostForm("next"), "/")
	c.Redirect(http.StatusFound, next)
}

func GuideWizardRestart(c *gin.Context) {
	userID := mustUserID(c)
	_ = model.ClearWizardDismissed(userID)
	c.Redirect(http.StatusFound, "/guides/getting-started")
}

func renderGuide(c *gin.Context, tmpl, title, pageTitle, subtitle string) {
	c.HTML(http.StatusOK, tmpl, gin.H{
		"title":     title,
		"pageTitle": pageTitle,
		"subtitle":  subtitle,
		"active":    "guides",
	})
}

func safeRelativePath(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return raw
	}
	return fallback
}
