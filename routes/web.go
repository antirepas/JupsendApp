package routes

import (
	"encoding/json"
	"fmt"
	"html/template"
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
		readyAccounts     []model.SMTPAccount
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
		readyAccounts, err = model.ListSendReadyAccountsForUser(userID)
		_ = err
		if len(readyAccounts) > 0 {
			acc = readyAccounts[0]
			for _, a := range readyAccounts {
				if a.IsDefault {
					acc = a
					break
				}
			}
		} else {
			acc, _ = model.GetSMTPAccountByUserID(userID)
		}
	}()
	wg.Wait()

	warmupProgress := outbound.ComputeCombinedWarmupProgress(readyAccounts)
	if !warmupProgress.HasAccount && acc.ID > 0 {
		warmupProgress = outbound.ComputeWarmupProgress(acc, true)
	}

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

	mailboxReady := acc.ID > 0 && acc.IsSendReady()
	isPro := model.UserIsPro(userID)
	plan := model.PlanInfoForTier(model.NormalizePlanTier(user.PlanTier))
	// Until the first email goes out, show setup — not blank analytics.
	isEmpty := stats.TotalSends == 0

	type setupStep struct {
		Key     string
		Label   string
		Hint    string
		Href    string
		Done    bool
		Primary bool
	}
	steps := []setupStep{
		{
			Key:   "mailbox",
			Label: "Ready to send",
			Hint:  "Shared mailbox connected",
			Href:  "/mailboxes",
			Done:  mailboxReady,
		},
		{
			Key:   "contacts",
			Label: "Add contacts",
			Hint:  "Import or paste your list",
			Href:  "/contacts?tab=import",
			Done:  counts.Contacts > 0,
		},
		{
			Key:   "template",
			Label: "Create a template",
			Hint:  "Subject, body, and a link to track",
			Href:  "/templates/new",
			Done:  counts.Templates > 0,
		},
		{
			Key:     "campaign",
			Label:   "Launch your first campaign",
			Hint:    "Pick a template, add contacts, send",
			Href:    "/campaigns/new",
			Done:    stats.TotalSends > 0,
			Primary: true,
		},
	}
	if isPro {
		steps[0].Hint = "Domain & mailboxes set up"
		if !mailboxReady {
			steps[0].Href = "/onboarding/domain"
			steps[0].Hint = "Set up your domain & included mailboxes"
		}
	}
	stepsLeft := 0
	nextHref := "/campaigns/new"
	for _, s := range steps {
		if !s.Done {
			stepsLeft++
			if stepsLeft == 1 {
				nextHref = s.Href
			}
		}
	}

	activityHasPrev := contactActivityPage.Page > 1
	activityHasNext := contactActivityPage.TotalPages > 0 && contactActivityPage.Page < contactActivityPage.TotalPages
	activityRangeStart := 0
	activityRangeEnd := 0
	if contactActivityPage.Total > 0 {
		activityRangeStart = (contactActivityPage.Page-1)*contactActivityPage.PageSize + 1
		activityRangeEnd = activityRangeStart + len(contactActivityPage.Items) - 1
	}

	ctx.HTML(http.StatusOK, "dashboard.html", gin.H{
		"title":               "Dashboard",
		"active":              "dashboard",
		"user":                user,
		"stats":               stats,
		"contactActivity":     contactActivityPage.Items,
		"contactActivityPage": contactActivityPage,
		"activityHasPrev":     activityHasPrev,
		"activityHasNext":     activityHasNext,
		"activityPrevPage":    contactActivityPage.Page - 1,
		"activityNextPage":    contactActivityPage.Page + 1,
		"activityRangeStart":  activityRangeStart,
		"activityRangeEnd":    activityRangeEnd,
		"dailyStats":          daily,
		"counts":              counts,
		"campaigns":           campaigns,
		"isEmpty":             isEmpty,
		"setupSteps":          steps,
		"stepsLeft":           stepsLeft,
		"nextSetupHref":       nextHref,
		"plan":                plan,
		"benchmark":           benchmark,
		"recentExperiments":   recentExperiments,
		"goalProgress":        goalProgress,
		"interestedCount":     interestedCount,
		"gmailConnected":      acc.IsGoogleOAuth(),
		"mailboxReady":        mailboxReady,
		"isPro":               isPro,
		"warmupProgress":      warmupProgress,
		"showWizardBanner":    !model.UserWizardDismissed(user),
		"playbook":            playbookAnalytics(),
		"success":             ctx.Query("success"),
		"error":               ctx.Query("error"),
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
		"playbook":  playbookTemplates(),
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewTemplatePage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	senderEmail, defaultSampleJSON := templateBuilderContext(userID)
	contacts, _ := model.ListContacts(userID)
	lists, _ := model.ListContactLists(userID)
	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":             "New Template",
		"active":            "templates",
		"isNew":             true,
		"senderEmail":       senderEmail,
		"defaultSampleJSON": defaultSampleJSON,
		"aiEnabled":         ai.Enabled(),
		"contacts":          contacts,
		"lists":             lists,
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
	contacts, _ := model.ListContacts(userID)
	lists, _ := model.ListContactLists(userID)
	ctx.HTML(http.StatusOK, "templates_form.html", gin.H{
		"title":             "Edit Template",
		"active":            "templates",
		"isNew":             false,
		"template":          t,
		"senderEmail":       senderEmail,
		"defaultSampleJSON": defaultSampleJSON,
		"aiEnabled":         ai.Enabled(),
		"contacts":          contacts,
		"lists":             lists,
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
	campaignID, _ := strconv.ParseInt(ctx.Query("campaign"), 10, 64)
	contacts, err := model.ListInterestedContactsFiltered(userID, campaignID, 200)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to load interested contacts"})
		return
	}
	lists, _ := model.ListContactLists(userID)
	campaigns, _ := model.ListCampaigns(userID)
	ctx.HTML(http.StatusOK, "contacts_interested.html", gin.H{
		"title":           "Interested contacts",
		"active":          "interested",
		"contacts":        contacts,
		"interestedCount": len(contacts),
		"lists":           lists,
		"campaigns":       campaigns,
		"filterCampaign":  campaignID,
		"playbook":        playbookInterested(),
		"success":         ctx.Query("success"),
		"error":           ctx.Query("error"),
	})
}

func ListContactsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	tab := ctx.DefaultQuery("tab", "all")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	listID, _ := strconv.ParseInt(ctx.Query("list"), 10, 64)
	campaignID, _ := strconv.ParseInt(ctx.Query("campaign"), 10, 64)
	engagement := ctx.Query("engagement")

	filter := model.ContactListFilter{
		Query:       ctx.Query("q"),
		ListID:      listID,
		CampaignID:  campaignID,
		Engagement:  engagement,
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
	campaigns, _ := model.ListCampaigns(userID)
	suppressions, _ := model.ListSuppressions(userID)
	allContacts, _ := model.ListContacts(userID)

	prevPage := page - 1
	nextPage := page + 1
	hasPrev := page > 1
	hasNext := page < contactPage.TotalPages

	ctx.HTML(http.StatusOK, "contacts_list.html", gin.H{
		"title":            "Contacts",
		"active":           "contacts",
		"tab":              tab,
		"contacts":         contactPage.Items,
		"allContacts":      allContacts,
		"contactPage":      contactPage,
		"templates":        templates,
		"lists":            lists,
		"campaigns":        campaigns,
		"suppressions":     suppressions,
		"filterQ":          filter.Query,
		"filterList":       listID,
		"filterCampaign":   campaignID,
		"filterEngagement": engagement,
		"filterSort":       filter.Sort,
		"filterReplied":    filter.RepliedOnly,
		"prevPage":         prevPage,
		"nextPage":         nextPage,
		"hasPrev":          hasPrev,
		"hasNext":          hasNext,
		"success":          ctx.Query("success"),
		"error":            ctx.Query("error"),
		"playbook":         playbookContacts(),
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

	colMap := parseColumnMapForm(ctx)
	var parsed []model.ImportContactRow
	if len(colMap) > 0 {
		utilRows, err := util.ParseContactPasteWithMap(paste, colMap)
		if err != nil {
			ctx.Redirect(http.StatusFound, "/contacts?tab=import&error="+url.QueryEscape(err.Error()))
			return
		}
		parsed = parseImportRowsFromExcel(utilRows, nil)
	} else {
		parsed = parseImportRowsFromPaste(paste, varKeys)
	}

	importKeys := varKeys
	if len(importKeys) == 0 && len(parsed) > 0 {
		importKeys = keysFromImportRowsParsed(parsed)
	}

	redir, _ := enqueueContactImport(userID, model.ImportKindContactsPaste, parsed, listID, importKeys, "/contacts?tab=import")
	ctx.Redirect(http.StatusFound, redir)
}

func BulkDeleteContacts(ctx *gin.Context) {
	userID := mustUserID(ctx)
	ids := parseContactIDs(ctx)
	n, _ := model.BulkDeleteContacts(userID, ids)
	msg := fmt.Sprintf("Deleted %d contacts", n)
	ctx.Redirect(http.StatusFound, "/contacts?success="+url.QueryEscape(msg))
}

func BulkAddContactsToList(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, _ := strconv.ParseInt(ctx.PostForm("list_id"), 10, 64)
	ids := parseContactIDs(ctx)
	redir := contactsFilterRedirect(ctx, "")
	if listID <= 0 || len(ids) == 0 {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape("Select contacts and a list"))
		return
	}
	if err := model.AddContactsToList(listID, userID, ids); err != nil {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape(err.Error()))
		return
	}
	ctx.Redirect(http.StatusFound, redir+"&success="+url.QueryEscape("Added "+strconv.Itoa(len(ids))+" contacts to list"))
}

func BulkAddMatchingContactsToList(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, _ := strconv.ParseInt(ctx.PostForm("list_id"), 10, 64)
	filter := model.ContactListFilter{
		Query:       ctx.PostForm("q"),
		ListID:      parseInt64Form(ctx, "list"),
		CampaignID:  parseInt64Form(ctx, "campaign"),
		Engagement:  ctx.PostForm("engagement"),
		Sort:        ctx.DefaultPostForm("sort", "newest"),
		RepliedOnly: ctx.PostForm("replied") == "1",
	}
	redir := contactsFilterRedirectFromFilter(filter, "")
	if listID <= 0 {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape("Choose a list"))
		return
	}
	ids, err := model.ListContactIDsMatching(userID, filter, 2000)
	if err != nil {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape(err.Error()))
		return
	}
	if len(ids) == 0 {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape("No contacts match these filters"))
		return
	}
	if err := model.AddContactsToList(listID, userID, ids); err != nil {
		ctx.Redirect(http.StatusFound, redir+"&error="+url.QueryEscape(err.Error()))
		return
	}
	msg := "Added " + strconv.Itoa(len(ids)) + " matching contacts to list"
	ctx.Redirect(http.StatusFound, redir+"&success="+url.QueryEscape(msg))
}

func parseInt64Form(ctx *gin.Context, key string) int64 {
	v, _ := strconv.ParseInt(ctx.PostForm(key), 10, 64)
	return v
}

func contactsFilterRedirect(ctx *gin.Context, _ string) string {
	q := url.Values{}
	q.Set("tab", "all")
	if v := ctx.PostForm("q"); v != "" {
		q.Set("q", v)
	}
	if v := ctx.PostForm("list"); v != "" {
		q.Set("list", v)
	}
	if v := ctx.PostForm("campaign"); v != "" {
		q.Set("campaign", v)
	}
	if v := ctx.PostForm("engagement"); v != "" {
		q.Set("engagement", v)
	}
	if v := ctx.PostForm("sort"); v != "" {
		q.Set("sort", v)
	}
	if ctx.PostForm("replied") == "1" {
		q.Set("replied", "1")
	}
	return "/contacts?" + q.Encode()
}

func contactsFilterRedirectFromFilter(f model.ContactListFilter, _ string) string {
	q := url.Values{}
	q.Set("tab", "all")
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.ListID > 0 {
		q.Set("list", strconv.FormatInt(f.ListID, 10))
	}
	if f.CampaignID > 0 {
		q.Set("campaign", strconv.FormatInt(f.CampaignID, 10))
	}
	if f.Engagement != "" {
		q.Set("engagement", f.Engagement)
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	if f.RepliedOnly {
		q.Set("replied", "1")
	}
	return "/contacts?" + q.Encode()
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
	conversation, _ := model.ListContactConversation(userID, id, 200)
	enrichLegacyConversationBodies(userID, id, conversation)
	replySubject := "Re: "
	if inbound, err := model.LatestInboundMessage(userID, id); err == nil {
		sub := strings.TrimSpace(inbound.Subject)
		if strings.HasPrefix(strings.ToLower(sub), "re:") {
			replySubject = sub
		} else if sub != "" {
			replySubject = "Re: " + sub
		}
	} else if len(summary.RecentSends) > 0 && summary.RecentSends[0].TemplateSubject != "" {
		replySubject = "Re: " + summary.RecentSends[0].TemplateSubject
	}
	canReply := model.CanReplyInApp(userID, id, summary.RepliedAt)
	replyFromEmail := ""
	if canReply {
		if acctID, err := model.LatestSMTPAccountForContact(userID, id); err == nil && acctID > 0 {
			if acc, err := model.GetSMTPAccount(acctID); err == nil && acc.UserID == userID {
				replyFromEmail = acc.SenderEmail()
			}
		}
		if replyFromEmail == "" {
			if acc, err := model.GetSendReadyAccountForUser(userID); err == nil {
				replyFromEmail = acc.SenderEmail()
			}
		}
	}

	ctx.HTML(http.StatusOK, "contact_detail.html", gin.H{
		"title":           summary.Contact.Email,
		"active":          "contacts",
		"summary":         summary,
		"listMemberships": listMemberships,
		"conversation":    conversation,
		"canReply":        canReply,
		"replyFromEmail":  replyFromEmail,
		"replySubject":    replySubject,
		"success":         ctx.Query("success"),
		"error":           ctx.Query("error"),
	})
}

// enrichLegacyConversationBodies re-renders template source with this lead's variables
// when an outbound message has no rendered snapshot yet.
func enrichLegacyConversationBodies(userID, contactID int64, msgs []model.ConversationMessage) {
	if len(msgs) == 0 {
		return
	}
	_, vars, err := model.GetContactForUser(contactID, userID)
	if err != nil {
		return
	}
	for i := range msgs {
		m := &msgs[i]
		if m.Direction != model.ConversationOutbound || m.EmailSendID <= 0 {
			continue
		}
		if strings.TrimSpace(m.BodyHTML) != "" || strings.TrimSpace(m.BodyText) != "" {
			if !strings.Contains(m.BodyHTML, "{{") && !strings.Contains(m.BodyText, "{{") && !strings.Contains(m.Subject, "{{") {
				continue
			}
		}
		detail, err := model.GetEmailSendDetail(m.EmailSendID)
		if err != nil || detail.TemplateID <= 0 {
			continue
		}
		if detail.RenderedHTML != "" || detail.RenderedText != "" {
			if detail.RenderedSubject != "" {
				m.Subject = detail.RenderedSubject
			}
			if detail.RenderedHTML != "" {
				m.BodyHTML = detail.RenderedHTML
			}
			if detail.RenderedText != "" {
				m.BodyText = detail.RenderedText
			}
			continue
		}
		tmpl, err := model.GetTemplate(detail.TemplateID)
		if err != nil {
			continue
		}
		subj, body, _, err := util.RenderEmail(tmpl.Subject, tmpl.Body, vars, util.RenderOptions{
			UserID:      userID,
			ForPreview:  true,
			MailboxVars: mailboxVarsForPreview(userID),
		})
		if err != nil {
			continue
		}
		m.Subject = subj
		m.BodyHTML = util.WrapHTMLBody(body)
		m.BodyText = util.StripHTML(m.BodyHTML)
	}
}

func ReplyContactPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	summary, err := model.GetContactSummary(userID, contactID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Contact+not+found")
		return
	}
	if !model.CanReplyInApp(userID, contactID, summary.RepliedAt) {
		ctx.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(contactID, 10)+"?error="+url.QueryEscape("Reply is not available for this contact yet")+"#conversation")
		return
	}

	targets, _ := model.ListReplyTargets(userID, contactID, 100)
	// Prefer rendered subjects on outbound targets that have a send snapshot.
	enrichLegacyConversationBodies(userID, contactID, targets)

	var selected *model.ConversationMessage
	if replyToID, err := strconv.ParseInt(ctx.Query("reply_to"), 10, 64); err == nil && replyToID > 0 {
		if msg, err := model.GetConversationMessageForUser(userID, contactID, replyToID); err == nil {
			selected = &msg
		}
	}
	if selected == nil {
		for i := range targets {
			if targets[i].Direction == model.ConversationInbound {
				selected = &targets[i]
				break
			}
		}
	}
	if selected == nil && len(targets) > 0 {
		selected = &targets[0]
	}
	if selected != nil {
		tmp := []model.ConversationMessage{*selected}
		enrichLegacyConversationBodies(userID, contactID, tmp)
		selected = &tmp[0]
		// Keep list in sync with selected subject's filled vars.
		for i := range targets {
			if targets[i].ID == selected.ID {
				targets[i] = *selected
				break
			}
		}
	}

	replySubject := "Re: "
	if selected != nil {
		sub := strings.TrimSpace(selected.Subject)
		if strings.HasPrefix(strings.ToLower(sub), "re:") {
			replySubject = sub
		} else if sub != "" {
			replySubject = "Re: " + sub
		}
	} else if len(summary.RecentSends) > 0 {
		sub := summary.RecentSends[0].RenderedSubject
		if sub == "" {
			sub = summary.RecentSends[0].TemplateSubject
		}
		if sub != "" {
			replySubject = "Re: " + sub
		}
	}

	senderEmail, _ := templateBuilderContext(userID)
	senderFromName := ""
	senderReason := "Default mailbox"
	if acctID, err := model.LatestSMTPAccountForContact(userID, contactID); err == nil && acctID > 0 {
		if acc, err := model.GetSMTPAccount(acctID); err == nil && acc.UserID == userID && acc.IsSendReady() {
			if e := acc.SenderEmail(); e != "" {
				senderEmail = e
				senderFromName = strings.TrimSpace(acc.FromName)
				senderReason = "Same mailbox as earlier sends to this lead"
			}
		}
	} else if acc, err := model.GetSendReadyAccountForUser(userID); err == nil {
		if e := acc.SenderEmail(); e != "" {
			senderEmail = e
			senderFromName = strings.TrimSpace(acc.FromName)
		}
	}
	senderLabel := senderEmail
	if senderFromName != "" {
		senderLabel = senderFromName + " <" + senderEmail + ">"
	}

	sample, _ := model.ContactVariableSample(userID, contactID)
	if sample == nil {
		sample = map[string]string{}
	}
	sampleJSON, _ := json.Marshal(sample)

	ctx.HTML(http.StatusOK, "contact_reply.html", gin.H{
		"title":             "Reply — " + summary.Contact.Email,
		"active":            "contacts",
		"contact":           summary.Contact,
		"replyTargets":      targets,
		"selectedReply":     selected,
		"replySubject":      replySubject,
		"senderEmail":       senderEmail,
		"senderLabel":       senderLabel,
		"senderReason":      senderReason,
		"defaultSampleJSON": template.JS(string(sampleJSON)),
		"aiEnabled":         ai.Enabled(),
		"error":             ctx.Query("error"),
	})
}

func ReplyContactWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	subject := strings.TrimSpace(ctx.PostForm("subject"))
	bodyHTML := strings.TrimSpace(ctx.PostForm("body"))
	bodyText := strings.TrimSpace(util.StripHTML(bodyHTML))
	replyToID, _ := strconv.ParseInt(ctx.PostForm("reply_to_id"), 10, 64)
	_, err = outbound.SendManualReply(outbound.ManualReplyInput{
		UserID:           userID,
		ContactID:        contactID,
		Subject:          subject,
		BodyText:         bodyText,
		BodyHTML:         bodyHTML,
		ReplyToMessageID: replyToID,
	})
	if err != nil {
		log.Print(err)
		q := "/contacts/" + strconv.FormatInt(contactID, 10) + "/reply?error=" + url.QueryEscape(err.Error())
		if replyToID > 0 {
			q += "&reply_to=" + strconv.FormatInt(replyToID, 10)
		}
		ctx.Redirect(http.StatusFound, q)
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(contactID, 10)+"?success="+url.QueryEscape("Reply sent")+"#conversation")
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
	pageNum, _ := strconv.Atoi(ctx.Query("page"))
	campaignID, _ := strconv.ParseInt(ctx.Query("campaign"), 10, 64)
	filter := model.SendListFilter{
		Status:     ctx.Query("status"),
		CampaignID: campaignID,
		Query:      ctx.Query("q"),
		Page:       pageNum,
		PageSize:   25,
	}
	page, err := model.ListEmailSendsFiltered(userID, filter)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "sends", "error": "Failed to load sends"})
		return
	}
	counts, _ := model.CountEmailSendsSummary(userID)
	campaigns, _ := model.ListCampaigns(userID)

	var prevPage, nextPage int
	if page.Page > 1 {
		prevPage = page.Page - 1
	}
	if page.Page < page.TotalPages {
		nextPage = page.Page + 1
	}

	ctx.HTML(http.StatusOK, "sends_list.html", gin.H{
		"title":            "Sends",
		"active":           "sends",
		"sends":            page.Items,
		"page":             page,
		"counts":           counts,
		"campaigns":        campaigns,
		"filterQ":          filter.Query,
		"filterStatus":     filter.Status,
		"filterCampaign":   filter.CampaignID,
		"prevPage":         prevPage,
		"nextPage":         nextPage,
		"success":          ctx.Query("success"),
		"error":            ctx.Query("error"),
		"gmailSendBlocked": model.GmailSendBlocked(userID),
		"playbook":         playbookSends(),
	})
}

func ClearCancelledSendsWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	n, err := model.ClearCancelledSends(userID)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/sends?error="+url.QueryEscape("Could not clear cancelled sends"))
		return
	}
	msg := fmt.Sprintf("Deleted %d cancelled sends", n)
	ctx.Redirect(http.StatusFound, "/sends?success="+url.QueryEscape(msg))
}

func DeleteSendWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/sends?error=Invalid+send")
		return
	}
	ok, err := model.DeleteEmailSendForUser(userID, id)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/sends?error="+url.QueryEscape("Could not delete send"))
		return
	}
	if !ok {
		ctx.Redirect(http.StatusFound, "/sends?error="+url.QueryEscape("Send not found or already delivered (delivered sends cannot be deleted)"))
		return
	}
	ctx.Redirect(http.StatusFound, "/sends?success="+url.QueryEscape("Send deleted"))
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

	renderedBody := template.HTML("")
	renderedBodySrcDoc := ""
	if detail.RenderedHTML != "" {
		safe := util.SanitizeHTMLForDisplay(model.RewriteTrackedClicksForDisplay(detail.RenderedHTML))
		renderedBody = template.HTML(safe)
		renderedBodySrcDoc = safe
	} else if detail.RenderedText != "" {
		safe := "<p>" + template.HTMLEscapeString(detail.RenderedText) + "</p>"
		renderedBody = template.HTML(safe)
		renderedBodySrcDoc = safe
	} else if detail.TemplateID > 0 {
		// Legacy: show template rendered with this contact's variables.
		if tmpl, err := model.GetTemplate(detail.TemplateID); err == nil {
			_, vars, _ := model.GetContactForUser(detail.ContactID, mustUserID(ctx))
			_, body, _, err := util.RenderEmail(tmpl.Subject, tmpl.Body, vars, util.RenderOptions{
				UserID:      mustUserID(ctx),
				ForPreview:  true,
				MailboxVars: mailboxVarsForPreview(mustUserID(ctx)),
			})
			if err == nil {
				safe := util.SanitizeHTMLForDisplay(util.WrapHTMLBody(body))
				renderedBody = template.HTML(safe)
				renderedBodySrcDoc = safe
			}
		}
	}

	ctx.HTML(http.StatusOK, "sends_detail.html", gin.H{
		"title":              "Send Detail",
		"active":             "sends",
		"send":               detail,
		"renderedBody":       renderedBody,
		"renderedBodySrcDoc": renderedBodySrcDoc,
		"success":            ctx.Query("success"),
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
