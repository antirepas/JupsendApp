package routes

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"emailtracker.com/ai"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func Dashboard(ctx *gin.Context) {
	userID := mustUserID(ctx)
	user, _ := model.GetUserByID(userID)

	activityPageNum, _ := strconv.Atoi(ctx.DefaultQuery("activity_page", "1"))
	if activityPageNum < 1 {
		activityPageNum = 1
	}
	const activityPageSize = 10

	var (
		stats               model.DashboardStats
		contactActivityPage model.ContactActivityPage
		daily               []model.DailyStat
		counts            model.EntityCounts
		campaigns         []model.CampaignListItem
		benchmark         model.AccountBenchmark
		recentExperiments []model.RecentExperiment
		repliesThisMonth  int
		interestedCount   int
		acc               model.SMTPAccount
		statsErr          error
	)

	var wg sync.WaitGroup
	wg.Add(10)
	go func() {
		defer wg.Done()
		stats, statsErr = model.GetDashboardStats(userID)
	}()
	go func() {
		defer wg.Done()
		contactActivityPage, _ = model.GetRecentContactActivityPage(userID, activityPageNum, activityPageSize)
	}()
	go func() {
		defer wg.Done()
		daily, _ = model.GetDailyStats(userID, 30)
	}()
	go func() {
		defer wg.Done()
		counts, _ = model.GetEntityCounts(userID)
	}()
	go func() {
		defer wg.Done()
		campaigns, _ = model.ListCampaigns(userID)
	}()
	go func() {
		defer wg.Done()
		benchmark = model.GetAccountBenchmark(userID, 30)
	}()
	go func() {
		defer wg.Done()
		recentExperiments, _ = model.ListRecentExperiments(userID, 5)
	}()
	go func() {
		defer wg.Done()
		repliesThisMonth = model.CountRepliesThisMonth(userID)
	}()
	go func() {
		defer wg.Done()
		interestedCount = model.CountInterestedContacts(userID)
	}()
	go func() {
		defer wg.Done()
		var err error
		acc, err = model.GetSMTPAccountByUserID(userID)
		_ = err
	}()
	wg.Wait()

	hasSMTPAccount := acc.ID > 0
	warmupProgress := outbound.ComputeWarmupProgress(acc, hasSMTPAccount)

	if statsErr != nil {
		log.Print(statsErr)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "dashboard", "error": "Failed to load stats"})
		return
	}

	goalProgress := util.ComputeGoalProgress(util.OutreachGoals{
		MeetingsPerMonth:  user.GoalMeetingsPerMonth,
		ReplyToMeetingPct: user.GoalReplyToMeetingPct,
		DailySendCap:      user.GoalDailySendCap,
	}, repliesThisMonth)

	isEmpty := stats.TotalSends == 0 && counts.Campaigns == 0

	activityHasPrev := contactActivityPage.Page > 1
	activityHasNext := contactActivityPage.TotalPages > 0 && contactActivityPage.Page < contactActivityPage.TotalPages
	activityRangeStart := 0
	activityRangeEnd := 0
	if contactActivityPage.Total > 0 {
		activityRangeStart = (contactActivityPage.Page-1)*contactActivityPage.PageSize + 1
		activityRangeEnd = activityRangeStart + len(contactActivityPage.Items) - 1
	}

	ctx.HTML(http.StatusOK, "dashboard.html", gin.H{
		"title":           "Dashboard",
		"active":          "dashboard",
		"user":            user,
		"stats":           stats,
		"contactActivity": contactActivityPage.Items,
		"contactActivityPage": contactActivityPage,
		"activityHasPrev":     activityHasPrev,
		"activityHasNext":     activityHasNext,
		"activityPrevPage":    contactActivityPage.Page - 1,
		"activityNextPage":    contactActivityPage.Page + 1,
		"activityRangeStart":  activityRangeStart,
		"activityRangeEnd":    activityRangeEnd,
		"dailyStats":      daily,
		"counts":          counts,
		"campaigns":       campaigns,
		"isEmpty":         isEmpty,
		"benchmark":       benchmark,
		"recentExperiments": recentExperiments,
		"goalProgress":    goalProgress,
		"interestedCount":   interestedCount,
		"gmailConnected":    acc.IsGoogleOAuth(),
		"warmupProgress":    warmupProgress,
		"success":         ctx.Query("success"),
		"error":           ctx.Query("error"),
	})
}

func ListTemplatesPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, err := model.ListTemplates(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to load templates"})
		return
	}
	ctx.HTML(http.StatusOK, "templates_list.html", gin.H{
		"title":     "Templates",
		"active":    "templates",
		"templates": templates,
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewTemplatePage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	senderEmail, defaultSampleJSON := templateBuilderContext(userID)
	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":             "New Template",
		"active":            "templates",
		"isNew":             true,
		"senderEmail":       senderEmail,
		"defaultSampleJSON": defaultSampleJSON,
		"aiEnabled":         ai.Enabled(),
	})
}

func CreateTemplate(ctx *gin.Context) {
	userID := mustUserID(ctx)
	name := ctx.PostForm("name")
	subject := ctx.PostForm("subject")
	body := ctx.PostForm("body")
	variables := util.ExtractTemplateVariables(subject, body)

	t := model.Template{Name: name, Subject: subject, Body: body}
	tv := make([]model.TemplateVariable, len(variables))
	for i, v := range variables {
		tv[i] = model.TemplateVariable{Key: v}
	}

	_, err := t.SaveTemplate(userID, tv)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to save template"})
		return
	}
	ctx.Redirect(http.StatusFound, "/templates?success=Template+created")
}

func EditTemplatePage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Invalid template ID"})
		return
	}

	t, _, err := model.GetTemplateByID(id, userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Template not found"})
		return
	}

	senderEmail, defaultSampleJSON := templateBuilderContext(userID)
	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":             "Edit Template",
		"active":            "templates",
		"isNew":             false,
		"template":          t,
		"senderEmail":       senderEmail,
		"defaultSampleJSON": defaultSampleJSON,
		"aiEnabled":         ai.Enabled(),
	})
}

func UpdateTemplate(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Invalid template ID"})
		return
	}

	name := ctx.PostForm("name")
	subject := ctx.PostForm("subject")
	body := ctx.PostForm("body")
	variables := util.ExtractTemplateVariables(subject, body)

	err = model.UpdateTemplate(id, userID, name, subject, body, variables)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "templates", "error": "Failed to update template"})
		return
	}
	ctx.Redirect(http.StatusFound, "/templates?success=Template+updated")
}

func InterestedContactsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contacts, err := model.ListInterestedContacts(userID, 100)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to load interested contacts"})
		return
	}
	ctx.HTML(http.StatusOK, "contacts_interested.html", gin.H{
		"title":    "Interested contacts",
		"active":   "contacts",
		"contacts": contacts,
	})
}

func ListContactsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	tab := ctx.DefaultQuery("tab", "all")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	listID, _ := strconv.ParseInt(ctx.Query("list"), 10, 64)

	filter := model.ContactListFilter{
		Query:       ctx.Query("q"),
		ListID:      listID,
		Sort:        ctx.DefaultQuery("sort", "newest"),
		Page:        page,
		PageSize:    50,
		RepliedOnly: ctx.Query("replied") == "1",
	}
	contactPage, err := model.ListContactsFiltered(userID, filter)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to load contacts"})
		return
	}

	templates, _ := model.ListTemplates(userID)
	lists, _ := model.ListContactLists(userID)
	suppressions, _ := model.ListSuppressions(userID)
	allContacts, _ := model.ListContacts(userID)

	prevPage := page - 1
	nextPage := page + 1
	hasPrev := page > 1
	hasNext := page < contactPage.TotalPages

	ctx.HTML(http.StatusOK, "contacts_list.html", gin.H{
		"title":        "Contacts",
		"active":       "contacts",
		"tab":          tab,
		"contacts":     contactPage.Items,
		"allContacts":  allContacts,
		"contactPage":  contactPage,
		"templates":    templates,
		"lists":        lists,
		"suppressions": suppressions,
		"filterQ":      filter.Query,
		"filterList":   listID,
		"filterSort":   filter.Sort,
		"filterReplied": filter.RepliedOnly,
		"prevPage":     prevPage,
		"nextPage":     nextPage,
		"hasPrev":      hasPrev,
		"hasNext":      hasNext,
		"success":      ctx.Query("success"),
		"error":        ctx.Query("error"),
	})
}

func NewContactPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, _ := model.ListTemplates(userID)
	templateID, _ := strconv.ParseInt(ctx.Query("template_id"), 10, 64)
	var templateVars []string
	if templateID > 0 {
		_, templateVars, _ = model.GetTemplateByID(templateID, userID)
	}
	ctx.HTML(http.StatusOK, "contacts_form.html", gin.H{
		"title":        "New Contact",
		"active":       "contacts",
		"templates":    templates,
		"templateID":   templateID,
		"templateVars": templateVars,
	})
}

func CreateContact(ctx *gin.Context) {
	userID := mustUserID(ctx)
	email := ctx.PostForm("email")
	keys := ctx.PostFormArray("var_key")
	values := ctx.PostFormArray("var_value")

	var cvs []model.ContactVariables
	for i, key := range keys {
		if key == "" {
			continue
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		cvs = append(cvs, model.ContactVariables{Key: key, Value: value})
	}

	c := model.Contact{Email: email}
	_, err := c.SaveContact(userID, cvs)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to save contact"})
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts?success=Contact+created")
}

func PasteContactsQuick(ctx *gin.Context) {
	userID := mustUserID(ctx)
	paste := ctx.PostForm("paste")
	listID, _ := strconv.ParseInt(ctx.PostForm("list_id"), 10, 64)
	templateID, _ := strconv.ParseInt(ctx.PostForm("template_id"), 10, 64)

	var varKeys []string
	if templateID > 0 {
		_, vars, err := model.GetTemplateByID(templateID, userID)
		if err == nil {
			varKeys = vars
		}
	}

	result, err := model.ImportContactRows(userID, parseImportRowsFromPaste(paste, varKeys), listID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Import+failed")
		return
	}
	msg := model.FormatImportResultMessage(result)
	ctx.Redirect(http.StatusFound, "/contacts?tab=import&success="+url.QueryEscape(msg))
}

func BulkDeleteContacts(ctx *gin.Context) {
	userID := mustUserID(ctx)
	ids := parseContactIDs(ctx)
	n, _ := model.BulkDeleteContacts(userID, ids)
	msg := fmt.Sprintf("Deleted %d contacts", n)
	ctx.Redirect(http.StatusFound, "/contacts?success="+url.QueryEscape(msg))
}

func ContactDetailPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	summary, err := model.GetContactSummary(userID, id)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Contact+not+found")
		return
	}
	allLists, _ := model.ListContactLists(userID)
	memberIDs := make(map[int64]bool)
	for _, l := range summary.Lists {
		memberIDs[l.ID] = true
	}
	type listMembership struct {
		List   model.ContactList
		Member bool
	}
	var listMemberships []listMembership
	for _, l := range allLists {
		listMemberships = append(listMemberships, listMembership{List: l, Member: memberIDs[l.ID]})
	}
	ctx.HTML(http.StatusOK, "contact_detail.html", gin.H{
		"title":           summary.Contact.Email,
		"active":          "contacts",
		"summary":         summary,
		"listMemberships": listMemberships,
		"success":         ctx.Query("success"),
		"error":           ctx.Query("error"),
	})
}

func UpdateContactLists(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	var listIDs []int64
	for _, s := range ctx.PostFormArray("list_ids") {
		id, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			listIDs = append(listIDs, id)
		}
	}
	if err := model.SetContactLists(userID, contactID, listIDs); err != nil {
		ctx.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(contactID, 10)+"?error=Could+not+update+lists")
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(contactID, 10)+"?success=Lists+updated")
}

func ListSendsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	sends, err := model.ListEmailSends(userID)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Failed to load sends"})
		return
	}
	ctx.HTML(http.StatusOK, "sends_list.html", gin.H{
		"title":            "Sends",
		"active":           "sends",
		"sends":            sends,
		"success":          ctx.Query("success"),
		"gmailSendBlocked": model.GmailSendBlocked(userID),
	})
}

func NewSendPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, err := model.ListTemplates(userID)
	if err != nil {
		log.Print(err)
	}
	contacts, err := model.ListContacts(userID)
	if err != nil {
		log.Print(err)
	}
	preselectedContactID, _ := strconv.ParseInt(ctx.Query("contact_id"), 10, 64)

	gmailEmail := ""
	if acc, err := model.GetSMTPAccountByUserID(userID); err == nil && acc.IsGoogleOAuth() {
		gmailEmail = acc.SenderEmail()
		if preselectedContactID == 0 && gmailEmail != "" {
			if id, err := model.FindOrCreateContact(userID, gmailEmail, nil); err == nil {
				preselectedContactID = id
			}
		}
	}

	ctx.HTML(http.StatusOK, "send_form.html", gin.H{
		"title":                "Send Email",
		"active":               "sends",
		"templates":            templates,
		"contacts":             contacts,
		"preselectedContactID": preselectedContactID,
		"gmailEmail":           gmailEmail,
		"gmailSendBlocked":     model.GmailSendBlocked(userID),
		"error":                ctx.Query("error"),
	})
}

func CreateSend(ctx *gin.Context) {
	templateID, err := strconv.ParseInt(ctx.PostForm("template_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/sends/new?error=Invalid+template")
		return
	}
	contactID, err := strconv.ParseInt(ctx.PostForm("contact_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/sends/new?error=Invalid+contact")
		return
	}

	emailSendID, err := processAndSendEmail(mustUserID(ctx), templateID, contactID, 0, "", 0)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/sends/new?error="+err.Error())
		return
	}
	ctx.Redirect(http.StatusFound, "/sends/"+strconv.FormatInt(emailSendID, 10)+"?success=Email+queued+for+delivery")
}

func SendDetailPage(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Invalid send ID"})
		return
	}

	detail, err := model.GetEmailSendDetailForUser(id, mustUserID(ctx))
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Send not found"})
		return
	}

	ctx.HTML(http.StatusOK, "sends_detail.html", gin.H{
		"title":  "Send Detail",
		"active": "sends",
		"send":   detail,
		"success": ctx.Query("success"),
	})
}

func parseLines(s string) []string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
