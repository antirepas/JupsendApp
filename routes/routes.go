package routes

import (
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
		"templates/error.html",
	)
	server.Static("/static", "./static")

	server.GET("/", Dashboard)
	server.GET("/templates", ListTemplatesPage)
	server.GET("/templates/new", NewTemplatePage)
	server.POST("/templates", CreateTemplate)
	server.GET("/templates/:id/edit", EditTemplatePage)
	server.POST("/templates/:id", UpdateTemplate)
	server.GET("/contacts", ListContactsPage)
	server.GET("/contacts/new", NewContactPage)
	server.POST("/contacts", CreateContact)
	server.GET("/contacts/upload/sample/:id", DownloadContactSample)
	server.POST("/contacts/upload", UploadContacts)
	server.GET("/sends", ListSendsPage)
	server.GET("/sends/new", NewSendPage)
	server.POST("/sends", CreateSend)
	server.GET("/sends/:id", SendDetailPage)

	v1 := server.Group("/api/v1")
	v1.GET("/track/open/:id", TrackOpen)
	v1.GET("/track/click/:id", TrackClick)
	v1.POST("/template", SaveTemplate)
	v1.POST("/contact", SaveContacts)
	v1.POST("/send", Email_send)
}
