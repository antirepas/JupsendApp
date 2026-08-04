package routes

import (
	"html/template"
	"net/http"

	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(server *gin.Engine) {
	server.SetFuncMap(template.FuncMap{
		"assetURL": func(path string) string {
			return path + "?v=" + util.StaticAssetVersion()
		},
	})
	server.LoadHTMLFiles(
		"templates/partials/head.html",
		"templates/partials/sidebar.html",
		"templates/partials/reply_pro_note.html",
		"templates/partials/workflow_archive_modal.html",
		"templates/partials/contact_import_map_modal.html",
		"templates/auth_login.html",
		"templates/auth_signup.html",
		"templates/pricing_plan.html",
		"templates/onboarding_activating.html",
		"templates/onboarding_domain.html",
		"templates/onboarding_domain_status.html",
		"templates/mailboxes.html",
		"templates/mailboxes_buy.html",
		"templates/mailboxes_buy_domain.html",
		"templates/settings.html",
		"templates/billing.html",
		"templates/dashboard.html",
		"templates/templates_list.html",
		"templates/templates_form.html",
		"templates/contacts_list.html",
		"templates/contacts_interested.html",
		"templates/contacts_list_detail.html",
		"templates/contacts_form.html",
		"templates/contact_detail.html",
		"templates/sends_list.html",
		"templates/send_form.html",
		"templates/sends_detail.html",
		"templates/campaigns_list.html",
		"templates/campaigns_form.html",
		"templates/campaigns_detail.html",
		"templates/campaigns_workflow_graph.html",
		"templates/campaigns_analytics.html",
		"templates/campaigns_workflow_analytics.html",
		"templates/campaigns_hybrid_analytics.html",
		"templates/workflows_list.html",
		"templates/workflows_form.html",
		"templates/workflows_builder.html",
		"templates/workflows_analytics.html",
		"templates/suppressions_list.html",
		"templates/guide_gmail.html",
		"templates/guide_contacts.html",
		"templates/guide_templates.html",
		"templates/guide_campaigns.html",
		"templates/guide_workflows.html",
		"templates/unsubscribe_confirm.html",
		"templates/error.html",
	)
	server.Static("/static", "./static")

	server.GET("/healthz", Healthz)

	server.GET("/login", RedirectIfAuthed(), LoginPage)
	server.GET("/signup", RedirectIfAuthed(), SignupPage)
	server.POST("/signup", RedirectIfAuthed(), SignupSubmit)
	server.POST("/logout", Logout)

	// Plan-first onboarding (3 separate URLs) + Google OAuth sign-in.
	server.GET("/signup/:plan", RedirectIfAuthed(), SignupPlanPage)
	server.GET("/auth/google/login", AppGoogleLoginStart)
	server.GET("/auth/google/start", AppGoogleStart)
	server.GET("/auth/google/callback", AppGoogleCallback)
	server.POST("/auth/dev/login", RedirectIfAuthed(), DevLogin)
	onboarding := server.Group("/")
	onboarding.Use(RequireAuth())
	{
		onboarding.GET("/onboarding/activate", OnboardingActivatePage)
	}

	mailboxOnboarding := server.Group("/")
	mailboxOnboarding.Use(RequireAuth(), RequireSubscription())
	{
		mailboxOnboarding.GET("/onboarding/domain", OnboardingDomainPage)
		mailboxOnboarding.POST("/onboarding/domain/search", OnboardingDomainSearch)
		mailboxOnboarding.POST("/onboarding/domain/purchase", OnboardingDomainPurchase)
		mailboxOnboarding.POST("/onboarding/domain/connect", OnboardingDomainConnect)
		mailboxOnboarding.GET("/onboarding/domain/status", OnboardingDomainStatus)
		mailboxOnboarding.GET("/mailboxes", MailboxesPage)
		mailboxOnboarding.POST("/mailboxes/attach", MailboxesAttachManual)
		mailboxOnboarding.GET("/mailboxes/buy", MailboxesBuyPage)
		mailboxOnboarding.POST("/mailboxes/buy/checkout", MailboxesBuyCheckout)
		mailboxOnboarding.GET("/mailboxes/domains/buy", MailboxesBuyDomainPage)
		mailboxOnboarding.POST("/mailboxes/domains/buy/search", MailboxesBuyDomainSearch)
		mailboxOnboarding.POST("/mailboxes/domains/buy/checkout", MailboxesBuyDomainCheckout)
		mailboxOnboarding.POST("/mailboxes/:id/default", MailboxesSetDefault)
		mailboxOnboarding.POST("/mailboxes/:id/from-name", MailboxesUpdateFromName)
	}

	v1 := server.Group("/api/v1")
	v1.GET("/track/open/:id", TrackOpen)
	v1.HEAD("/track/open/:id", TrackOpen)
	v1.GET("/track/click/:id", TrackClick)
	v1.HEAD("/track/click/:id", TrackClick)
	v1.POST("/webhooks/whop", WhopWebhook)
	v1.GET("/u/:token", UnsubscribePage)

	// Public aliases (no /api/v1 prefix) for tracking and unsubscribe links.
	server.GET("/track/open/:id", TrackOpen)
	server.HEAD("/track/open/:id", TrackOpen)
	server.GET("/track/click/:id", TrackClick)
	server.HEAD("/track/click/:id", TrackClick)
	server.GET("/u/:token", UnsubscribePage)

	settings := server.Group("/")
	settings.Use(RequireAuth())
	{
		settings.GET("/settings", SettingsPage)
		settings.POST("/settings", UpdateSettings)
		settings.GET("/settings/billing", BillingPage)
		settings.POST("/settings/billing/checkout", BillingCheckout)
		settings.POST("/settings/billing/cancel", BillingCancel)
		settings.GET("/settings/gmail/connect", GmailConnect)
		settings.GET("/settings/gmail/callback", GmailCallback)
		settings.GET("/settings/smtp-check", SettingsSMTPCheck)

		settings.GET("/guides/gmail", GuideGmail)
		settings.GET("/guides/contacts", GuideContacts)
		settings.GET("/guides/templates", GuideTemplates)
		settings.GET("/guides/campaigns", GuideCampaigns)
		settings.GET("/guides/workflows", GuideWorkflows)
	}

	authd := server.Group("/")
	authd.Use(RequireAuth(), RequireSubscription(), RequireMailboxSetup())
	{
		authd.GET("/", Dashboard)
		authd.GET("/test-pixel", TestTrackingPixel)

		authd.GET("/templates", ListTemplatesPage)
		authd.GET("/templates/new", NewTemplatePage)
		authd.POST("/templates/preview", PreviewTemplate)
		authd.POST("/templates/lint", TemplateLint)
		authd.POST("/templates/ai/rewrite", TemplateAIRewrite)
		authd.POST("/templates/ai/subject-alternatives", TemplateAISubjectAlternatives)
		authd.POST("/templates/ai/personalization-hint", TemplateAIPersonalizationHint)
		authd.POST("/templates/ai/tone-check", TemplateAIToneCheck)
		authd.POST("/templates/ai/starters", TemplateAIStarters)
		authd.POST("/templates/ai/soften-body", TemplateAISoftenBody)
		authd.POST("/templates", CreateTemplate)
		authd.GET("/templates/:id/edit", EditTemplatePage)
		authd.POST("/templates/:id", UpdateTemplate)
		authd.POST("/templates/:id/delete", DeleteTemplate)

		authd.GET("/contacts", ListContactsPage)
		authd.GET("/contacts/interested", InterestedContactsPage)
		authd.GET("/contacts/new", NewContactPage)
		authd.GET("/contacts/suppressions", ListSuppressionsPage)
		authd.POST("/contacts/suppressions", AddSuppressionWeb)
		authd.POST("/contacts/suppressions/:contact_id/remove", RemoveSuppressionWeb)
		authd.POST("/contacts/lists", CreateContactList)
		authd.POST("/contacts/lists/:id/rename", RenameContactList)
		authd.POST("/contacts/lists/:id/delete", DeleteContactList)
		authd.POST("/contacts/lists/:id/members", AddListMembers)
		authd.POST("/contacts/lists/:id/members/:contact_id/remove", RemoveListMember)
		authd.POST("/contacts/lists/:id/schema", SetContactListSchema)
		authd.GET("/contacts/lists/:id/variables", ListVariablesJSON)
		authd.GET("/contacts/lists/:id", ContactListDetailPage)
		authd.POST("/contacts/interested/bulk-add-list", InterestedBulkAddList)
		authd.POST("/contacts/interested/bulk-suppress", InterestedBulkSuppress)
		authd.POST("/contacts/interested/bulk-dismiss", InterestedBulkDismiss)
		authd.POST("/contacts/bulk-delete", BulkDeleteContacts)
		authd.POST("/contacts/validate", ValidateContactsWeb)
		authd.POST("/contacts/paste", PasteContactsQuick)
		authd.POST("/contacts/paste/preview", PreviewContactsPaste)
		authd.GET("/contacts/paste", func(c *gin.Context) { c.Redirect(http.StatusFound, "/contacts") })
		authd.GET("/contacts/upload/sample/:id", DownloadContactSample)
		authd.GET("/contacts/upload/sample", DownloadContactSample)
		authd.POST("/contacts/upload/preview", PreviewContactsUpload)
		authd.POST("/contacts/upload", UploadContacts)
		authd.GET("/contacts/:id/variables", ContactVariablesJSON)
		authd.GET("/contacts/:id/edit", EditContactPage)
		authd.GET("/contacts/:id", ContactDetailPage)
		authd.POST("/contacts/:id/lists", UpdateContactLists)
		authd.POST("/contacts/:id/reply", ReplyContactWeb)
		authd.POST("/contacts", CreateContact)
		authd.POST("/contacts/:id", UpdateContact)
		authd.POST("/contacts/:id/delete", DeleteContact)

		authd.GET("/suppressions", RedirectSuppressions)
		authd.POST("/suppressions", AddSuppressionWeb)
		authd.POST("/suppressions/:contact_id/remove", RemoveSuppressionWeb)

		authd.GET("/workflows", ListWorkflowsPage)
		authd.GET("/workflows/new", NewWorkflowPage)
		authd.POST("/workflows", CreateWorkflowWeb)
		authd.GET("/workflows/:id/archive-preview", WorkflowArchivePreviewJSON)
		authd.POST("/workflows/:id/archive", ArchiveWorkflowWeb)
		authd.POST("/workflows/:id/delete", ArchiveWorkflowWeb)
		authd.GET("/workflows/:id/edit", WorkflowBuilderPage)
		authd.GET("/workflows/:id/analytics", WorkflowAnalyticsPage)

		authd.GET("/campaigns", ListCampaignsPage)
		authd.GET("/campaigns/new", NewCampaignPage)
		authd.POST("/campaigns", CreateCampaign)
		authd.POST("/campaigns/:id/delete", DeleteCampaign)
		authd.POST("/campaigns/:id/schedule", ScheduleCampaign)
		authd.POST("/campaigns/:id/cancel-schedule", CancelCampaignSchedule)
		authd.GET("/campaigns/:id/analytics", CampaignAnalyticsPage)
		authd.POST("/campaigns/:id/promote-winner", PromoteCampaignWinner)
		authd.GET("/campaigns/:id", CampaignDetailPage)
		authd.POST("/campaigns/:id/contacts", AddCampaignContacts)
		authd.POST("/campaigns/:id/contacts/remove", RemoveCampaignContacts)
		authd.POST("/campaigns/:id/contacts/remove-missing", RemoveCampaignContactsMissingVars)
		authd.POST("/campaigns/:id/add-list", AddCampaignList)
		authd.POST("/campaigns/:id/refresh-list", RefreshCampaignList)
		authd.POST("/campaigns/:id/paste", PasteCampaignContacts)
		authd.POST("/campaigns/:id/upload", UploadCampaignContacts)
		authd.GET("/campaigns/:id/sample", DownloadCampaignSample)
		authd.POST("/campaigns/:id/workflow-templates", SaveCampaignWorkflowTemplatesWeb)
		authd.POST("/campaigns/:id/send", SendCampaign)
		authd.POST("/campaigns/:id/stop", StopCampaign)

		authd.GET("/sends", ListSendsPage)
		authd.GET("/sends/new", NewSendPage)
		authd.POST("/sends", CreateSend)
		authd.POST("/sends/clear-cancelled", ClearCancelledSendsWeb)
		authd.POST("/sends/:id/delete", DeleteSendWeb)
		authd.GET("/sends/:id", SendDetailPage)
	}

	api := server.Group("/api/v1")
	api.Use(RequireAuth(), RequireSubscription(), RequireMailboxSetup())
	{
		api.POST("/template", SaveTemplate)
		api.POST("/contact", SaveContacts)
		api.POST("/send", Email_send)
		api.GET("/send-jobs", GetSendJobsAPI)
		api.GET("/import-jobs", ListImportJobsAPI)
		api.POST("/import-jobs/:id/dismiss", DismissImportJobAPI)
		api.GET("/smtp-check", OpsSMTPCheck)
		RegisterWorkflowAPI(api)
	}

	ops := server.Group("/api/v1/ops")
	ops.Use(RequireAuth(), RequireAdmin())
	{
		ops.GET("/queue", OpsQueue)
	}
}
