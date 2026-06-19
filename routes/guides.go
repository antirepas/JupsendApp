package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func GuideGmail(c *gin.Context) {
	renderGuide(c, "guide_gmail.html", "Connect Gmail", "How to connect Gmail", "Send email and detect bounces from your own Gmail account using OAuth.")
}

func GuideTemplates(c *gin.Context) {
	renderGuide(c, "guide_templates.html", "Email templates", "How templates work", "Reusable email designs with personalization variables.")
}

func GuideCampaigns(c *gin.Context) {
	renderGuide(c, "guide_campaigns.html", "Campaigns", "How campaigns work", "Send personalized email to many contacts and track results.")
}

func GuideWorkflows(c *gin.Context) {
	renderGuide(c, "guide_workflows.html", "Workflows", "How workflows work", "Automate follow-ups based on opens, clicks, and time delays.")
}

func renderGuide(c *gin.Context, tmpl, title, pageTitle, subtitle string) {
	c.HTML(http.StatusOK, tmpl, gin.H{
		"title":     title,
		"pageTitle": pageTitle,
		"subtitle":  subtitle,
		"active":    "dashboard",
	})
}
