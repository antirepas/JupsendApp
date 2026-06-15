package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(server *gin.Engine) {
	server.LoadHTMLFiles(
		"templates/partials/head.html",
		"templates/partials/sidebar.html",
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
		"templates/error.html",
	)
	server.Static("/static", "./static")

	server.GET("/", Dashboard)
	server.POST("/settings/base-url", UpdateBaseURL)
	server.GET("/test-pixel", TestTrackingPixel)

	server.GET("/templates", ListTemplatesPage)
	server.GET("/templates/new", NewTemplatePage)
	server.POST("/templates", CreateTemplate)
	server.GET("/templates/:id/edit", EditTemplatePage)
	server.POST("/templates/:id", UpdateTemplate)
	server.POST("/templates/:id/delete", DeleteTemplate)

	server.GET("/contacts", ListContactsPage)
	server.GET("/contacts/new", NewContactPage)
	server.GET("/contacts/:id/edit", EditContactPage)
	server.POST("/contacts", CreateContact)
	server.POST("/contacts/:id", UpdateContact)
	server.POST("/contacts/:id/delete", DeleteContact)
	server.POST("/contacts/paste", PasteContactsQuick)
	server.GET("/contacts/paste", func(c *gin.Context) { c.Redirect(http.StatusFound, "/contacts") })
	server.GET("/contacts/upload/sample/:id", DownloadContactSample)
	server.POST("/contacts/upload", UploadContacts)

	server.GET("/workflows", ListWorkflowsPage)
	server.GET("/workflows/new", NewWorkflowPage)
	server.POST("/workflows", CreateWorkflowWeb)
	server.GET("/workflows/:id/edit", WorkflowBuilderPage)
	server.GET("/workflows/:id/analytics", WorkflowAnalyticsPage)

	server.GET("/campaigns", ListCampaignsPage)
	server.GET("/campaigns/new", NewCampaignPage)
	server.POST("/campaigns", CreateCampaign)
	server.POST("/campaigns/:id/delete", DeleteCampaign)
	server.POST("/campaigns/:id/schedule", ScheduleCampaign)
	server.POST("/campaigns/:id/cancel-schedule", CancelCampaignSchedule)
	server.GET("/campaigns/:id/analytics", CampaignAnalyticsPage)
	server.GET("/campaigns/:id", CampaignDetailPage)
	server.POST("/campaigns/:id/contacts", AddCampaignContacts)
	server.POST("/campaigns/:id/paste", PasteCampaignContacts)
	server.POST("/campaigns/:id/upload", UploadCampaignContacts)
	server.GET("/campaigns/:id/sample", DownloadCampaignSample)
	server.POST("/campaigns/:id/send", SendCampaign)

	server.GET("/sends", ListSendsPage)
	server.GET("/sends/new", NewSendPage)
	server.POST("/sends", CreateSend)
	server.GET("/sends/:id", SendDetailPage)

	v1 := server.Group("/api/v1")
	v1.GET("/track/open/:id", TrackOpen)
	v1.HEAD("/track/open/:id", TrackOpen)
	v1.GET("/track/click/:id", TrackClick)
	v1.HEAD("/track/click/:id", TrackClick)
	v1.POST("/template", SaveTemplate)
	v1.POST("/contact", SaveContacts)
	v1.POST("/send", Email_send)
	RegisterWorkflowAPI(v1)
}
