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
		"subtitle":  "Subscribe, explore the app, then add mailboxes when you’re ready to send.",
		"active":    "getting-started",
	})
}

func GuideGmail(c *gin.Context) {
	renderGuide(c, "guide_gmail.html", "Google sign-in", "Google sign-in", "Google is for signing in. Outreach sending is configured under Mailboxes.")
}

func GuideContacts(c *gin.Context) {
	renderGuide(c, "guide_contacts.html", "Contacts", "Contacts", "Sheets in campaign folders, variables, suppressions, and plan contact caps.")
}

func GuideTemplates(c *gin.Context) {
	renderGuide(c, "guide_templates.html", "Templates", "Templates", "Merge tags, Jev review, and copy that belongs in a campaign folder.")
}

func GuideCampaigns(c *gin.Context) {
	renderGuide(c, "guide_campaigns.html", "Campaigns", "Campaigns", "Folders, one-time vs sequence, local-time schedule, and launch checklist.")
}

func GuideWorkflows(c *gin.Context) {
	renderGuide(c, "guide_workflows.html", "Sequences", "Sequences & follow-ups", "Edit sequence on Manage, reply watchers, stop rules — and what happened to legacy workflows.")
}

func GuideOutreach(c *gin.Context) {
	// Old “recommended playbook” URL — keep bookmark-safe redirect.
	c.Redirect(http.StatusFound, "/guides")
}

func GuideMailboxes(c *gin.Context) {
	renderGuide(c, "guide_mailboxes.html", "Mailboxes", "Mailboxes", "Domain add-ons, seats, and deliverability — set up when you’re ready to send.")
}

func GuideSends(c *gin.Context) {
	renderGuide(c, "guide_sends.html", "Sends", "Sends", "Queue status, human vs bot opens, and debugging a single email.")
}

func GuideInterested(c *gin.Context) {
	renderGuide(c, "guide_interested.html", "Interested contacts", "Interested contacts", "Prioritize positive replies and close while interest is fresh.")
}

func GuideAnalytics(c *gin.Context) {
	renderGuide(c, "guide_analytics.html", "Analytics", "Analytics", "Optimize for replies; use opens/clicks as diagnostics.")
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
