package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(server *gin.Engine) {
	server.LoadHTMLFiles(
		"templates/partials/head.html",
		"templates/partials/sidebar.html",
		"templates/auth_login.html",
		"templates/auth_signup.html",
		"templates/settings.html",
		"templates/billing.html",
		"templates/dashboard.html",
		"templates/templates_list.html",
		"templates/templates_form.html",
		"templates/contacts_list.html",
		"templates/contacts_form.html",
		"templates/sends_list.html",
		"templates/send_form.html",
		"templates/sends_detail.html",
		"templates/campaigns_list.html",
		"templates/campaigns_form.html",
		"templates/campaigns_detail.html",
		"templates/campaigns_analytics.html",
		"templates/workflows_list.html",
		"templates/workflows_form.html",
		"templates/workflows_builder.html",
		"templates/workflows_analytics.html",
		"templates/suppressions_list.html",
		"templates/guide_gmail.html",
		"templates/guide_templates.html",
		"templates/guide_campaigns.html",
		"templates/guide_workflows.html",
		"templates/error.html",
	)
	server.Static("/static", "./static")

	server.GET("/login", RedirectIfAuthed(), LoginPage)
	server.POST("/login", RedirectIfAuthed(), LoginSubmit)
	server.GET("/signup", RedirectIfAuthed(), SignupPage)
	server.POST("/signup", RedirectIfAuthed(), SignupSubmit)
	server.POST("/logout", Logout)

	v1 := server.Group("/api/v1")
	v1.GET("/track/open/:id", TrackOpen)
	v1.HEAD("/track/open/:id", TrackOpen)
	v1.GET("/track/click/:id", TrackClick)
	v1.HEAD("/track/click/:id", TrackClick)
	v1.POST("/webhooks/whop", WhopWebhook)

	settings := server.Group("/")
	settings.Use(RequireAuth())
	{
		settings.GET("/settings", SettingsPage)
		settings.POST("/settings", UpdateSettings)
		settings.POST("/settings/base-url", UpdateBaseURL)
		settings.GET("/settings/billing", BillingPage)
		settings.POST("/settings/billing/checkout", BillingCheckout)
		settings.GET("/settings/gmail/connect", GmailConnect)
		settings.GET("/settings/gmail/callback", GmailCallback)
		settings.POST("/settings/gmail/disconnect", GmailDisconnect)

		settings.GET("/guides/gmail", GuideGmail)
		settings.GET("/guides/templates", GuideTemplates)
		settings.GET("/guides/campaigns", GuideCampaigns)
		settings.GET("/guides/workflows", GuideWorkflows)
	}

	authd := server.Group("/")
	authd.Use(RequireAuth(), RequireSubscription())
	{
		authd.GET("/", Dashboard)
		authd.GET("/test-pixel", TestTrackingPixel)

		authd.GET("/templates", ListTemplatesPage)
		authd.GET("/templates/new", NewTemplatePage)
		authd.POST("/templates", CreateTemplate)
		authd.GET("/templates/:id/edit", EditTemplatePage)
		authd.POST("/templates/:id", UpdateTemplate)
		authd.POST("/templates/:id/delete", DeleteTemplate)

		authd.GET("/contacts", ListContactsPage)
		authd.GET("/contacts/new", NewContactPage)
		authd.GET("/contacts/suppressions", ListSuppressionsPage)
		authd.POST("/contacts/suppressions", AddSuppressionWeb)
		authd.POST("/contacts/suppressions/:contact_id/remove", RemoveSuppressionWeb)
		authd.GET("/contacts/:id/edit", EditContactPage)
		authd.POST("/contacts", CreateContact)
		authd.POST("/contacts/:id", UpdateContact)
		authd.POST("/contacts/:id/delete", DeleteContact)
		authd.POST("/contacts/paste", PasteContactsQuick)
		authd.GET("/contacts/paste", func(c *gin.Context) { c.Redirect(http.StatusFound, "/contacts") })
		authd.GET("/contacts/upload/sample/:id", DownloadContactSample)
		authd.POST("/contacts/upload", UploadContacts)

		authd.GET("/suppressions", RedirectSuppressions)
		authd.POST("/suppressions", AddSuppressionWeb)
		authd.POST("/suppressions/:contact_id/remove", RemoveSuppressionWeb)

		authd.GET("/workflows", ListWorkflowsPage)
		authd.GET("/workflows/new", NewWorkflowPage)
		authd.POST("/workflows", CreateWorkflowWeb)
		authd.GET("/workflows/:id/edit", WorkflowBuilderPage)
		authd.GET("/workflows/:id/analytics", WorkflowAnalyticsPage)

		authd.GET("/campaigns", ListCampaignsPage)
		authd.GET("/campaigns/new", NewCampaignPage)
		authd.POST("/campaigns", CreateCampaign)
		authd.POST("/campaigns/:id/delete", DeleteCampaign)
		authd.POST("/campaigns/:id/schedule", ScheduleCampaign)
		authd.POST("/campaigns/:id/cancel-schedule", CancelCampaignSchedule)
		authd.GET("/campaigns/:id/analytics", CampaignAnalyticsPage)
		authd.GET("/campaigns/:id", CampaignDetailPage)
		authd.POST("/campaigns/:id/contacts", AddCampaignContacts)
		authd.POST("/campaigns/:id/paste", PasteCampaignContacts)
		authd.POST("/campaigns/:id/upload", UploadCampaignContacts)
		authd.GET("/campaigns/:id/sample", DownloadCampaignSample)
		authd.POST("/campaigns/:id/send", SendCampaign)

		authd.GET("/sends", ListSendsPage)
		authd.GET("/sends/new", NewSendPage)
		authd.POST("/sends", CreateSend)
		authd.GET("/sends/:id", SendDetailPage)
	}

	api := server.Group("/api/v1")
	api.Use(RequireAuth(), RequireSubscription())
	{
		api.POST("/template", SaveTemplate)
		api.POST("/contact", SaveContacts)
		api.POST("/send", Email_send)
		api.GET("/send-jobs", GetSendJobsAPI)
		RegisterWorkflowAPI(api)
	}
}
