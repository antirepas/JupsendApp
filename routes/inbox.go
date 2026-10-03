package routes

import (
	"html"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func inboxReturnURL(folder string, contactID int64, extra ...string) string {
	q := url.Values{}
	folder = model.NormalizeInboxFolder(folder)
	if folder != model.InboxFolderAll {
		q.Set("folder", folder)
	}
	if contactID > 0 {
		q.Set("contact", strconv.FormatInt(contactID, 10))
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if extra[i] != "" && extra[i+1] != "" {
			q.Set(extra[i], extra[i+1])
		}
	}
	if enc := q.Encode(); enc != "" {
		return "/inbox?" + enc
	}
	return "/inbox"
}

func inboxFolderTitle(folder string) string {
	switch model.NormalizeInboxFolder(folder) {
	case model.InboxFolderUnread:
		return "Unread"
	case model.InboxFolderInterested:
		return "Interested"
	case model.InboxFolderOpened:
		return "Opened"
	case model.InboxFolderClicked:
		return "Clicked"
	case model.InboxFolderSent:
		return "Sent"
	default:
		return "Inbox"
	}
}

func loadInboxSelected(userID, contactID int64, folder string) (detail *model.InboxThreadDetail, replySubject, replyFrom string, err error) {
	detail, err = model.GetInboxThread(userID, contactID)
	if err != nil {
		return nil, "", "", err
	}
	enrichLegacyConversationBodies(userID, contactID, detail.Messages)
	repairMangledConversationBodies(detail.Messages)
	replySubject = detail.Subject
	if !strings.HasPrefix(strings.ToLower(replySubject), "re:") {
		replySubject = "Re: " + replySubject
	}
	if accID, accErr := model.LatestSMTPAccountForContact(userID, contactID); accErr == nil && accID > 0 {
		if acc, accErr := model.GetSMTPAccount(accID); accErr == nil {
			replyFrom = acc.SenderEmail()
		}
	}
	_ = folder
	return detail, replySubject, replyFrom, nil
}

func InboxPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	folder := model.NormalizeInboxFolder(ctx.Query("folder"))
	q := strings.TrimSpace(ctx.Query("q"))
	contactID, _ := strconv.ParseInt(ctx.Query("contact"), 10, 64)
	sendID, _ := strconv.ParseInt(ctx.Query("send"), 10, 64)
	sendStatus := strings.TrimSpace(ctx.Query("status"))

	var (
		threads      []model.InboxThread
		sends        []model.EmailSendListItem
		sendPage     model.SendListPage
		threadsErr   error
		sendsErr     error
		folderCounts model.InboxFolderCounts
		selected     *model.InboxThreadDetail
		selectedID   int64
		selectedSend int64
		replySubject string
		replyFrom    string
		selectedErr  error
	)

	if sendID > 0 {
		detail, err := model.GetEmailSendDetailForUser(sendID, userID)
		if err != nil {
			ctx.Redirect(http.StatusFound, inboxReturnURL(folder, 0, "error", "Send not found"))
			return
		}
		contactID = detail.ContactID
		selectedSend = sendID
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		folderCounts = model.CountInboxFolders(userID)
		if folder == model.InboxFolderSent {
			pageNum, _ := strconv.Atoi(ctx.Query("page"))
			sendPage, sendsErr = model.ListEmailSendsFiltered(userID, model.SendListFilter{
				Status:   sendStatus,
				Query:    q,
				Page:     pageNum,
				PageSize: 75,
			})
			sends = sendPage.Items
			return
		}
		threads, threadsErr = model.ListInboxThreads(userID, folder, q, 75)
	}()
	go func() {
		defer wg.Done()
		if contactID <= 0 {
			return
		}
		selected, replySubject, replyFrom, selectedErr = loadInboxSelected(userID, contactID, folder)
		if selectedErr == nil {
			selectedID = contactID
		}
	}()
	wg.Wait()

	if folder == model.InboxFolderSent {
		if sendsErr != nil {
			log.Print(sendsErr)
			ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"title": "Error", "active": "inbox", "error": "Failed to load sends",
			})
			return
		}
	} else if threadsErr != nil {
		log.Print(threadsErr)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"title": "Error", "active": "inbox", "error": "Failed to load inbox",
		})
		return
	}
	if (contactID > 0 || sendID > 0) && selectedErr != nil {
		log.Print(selectedErr)
		ctx.Redirect(http.StatusFound, inboxReturnURL(folder, 0, "error", "Thread not found"))
		return
	}
	for i := range threads {
		if threads[i].ContactID == selectedID {
			threads[i].Unread = false
		}
	}

	ctx.HTML(http.StatusOK, "inbox.html", gin.H{
		"title":         "Inbox",
		"active":        "inbox",
		"folder":        folder,
		"folderTitle":   inboxFolderTitle(folder),
		"q":             q,
		"threads":       threads,
		"sends":         sends,
		"sendPage":      sendPage,
		"sendStatus":    sendStatus,
		"folderCounts":  folderCounts.Map(),
		"inboxUnread":   folderCounts.Unread,
		"selected":      selected,
		"selectedID":    selectedID,
		"selectedSend":  selectedSend,
		"replySubject":  replySubject,
		"replyFrom":     replyFrom,
		"success":       ctx.Query("success"),
		"error":         ctx.Query("error"),
	})
}

func InboxThreadPage(ctx *gin.Context) {
	contactID, err := strconv.ParseInt(ctx.Param("contactId"), 10, 64)
	if err != nil || contactID <= 0 {
		ctx.Redirect(http.StatusFound, "/inbox?error=Invalid+thread")
		return
	}
	folder := model.NormalizeInboxFolder(ctx.Query("folder"))
	ctx.Redirect(http.StatusFound, inboxReturnURL(folder, contactID))
}

func InboxThreadPane(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("contactId"), 10, 64)
	if err != nil || contactID <= 0 {
		ctx.String(http.StatusBadRequest, "Invalid thread")
		return
	}
	folder := model.NormalizeInboxFolder(ctx.Query("folder"))
	selected, replySubject, replyFrom, err := loadInboxSelected(userID, contactID, folder)
	if err != nil {
		ctx.String(http.StatusNotFound, "Thread not found")
		return
	}
	ctx.Header("X-Inbox-Unread", strconv.Itoa(model.CountInboxUnread(userID)))
	ctx.HTML(http.StatusOK, "inbox_pane", gin.H{
		"folder":       folder,
		"selected":     selected,
		"selectedID":   contactID,
		"replySubject": replySubject,
		"replyFrom":    replyFrom,
	})
}

func InboxMarkRead(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("contactId"), 10, 64)
	if err != nil || contactID <= 0 {
		ctx.Redirect(http.StatusFound, "/inbox?error=Invalid+thread")
		return
	}
	_ = model.MarkInboxThreadRead(userID, contactID)
	folder := model.NormalizeInboxFolder(ctx.Query("folder"))
	if ctx.GetHeader("HX-Request") != "" || strings.Contains(ctx.GetHeader("Accept"), "application/json") {
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "unread": model.CountInboxUnread(userID)})
		return
	}
	ctx.Redirect(http.StatusFound, inboxReturnURL(folder, contactID))
}

func InboxReplyWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("contactId"), 10, 64)
	if err != nil || contactID <= 0 {
		ctx.Redirect(http.StatusFound, "/inbox?error=Invalid+thread")
		return
	}
	folder := model.NormalizeInboxFolder(ctx.PostForm("folder"))
	if folder == model.InboxFolderAll {
		folder = model.NormalizeInboxFolder(ctx.Query("folder"))
	}
	subject := strings.TrimSpace(ctx.PostForm("subject"))
	bodyHTML := strings.TrimSpace(ctx.PostForm("body"))
	if bodyHTML == "" {
		bodyText := strings.TrimSpace(ctx.PostForm("body_text"))
		if bodyText != "" {
			bodyHTML = "<p>" + html.EscapeString(bodyText) + "</p>"
		}
	}
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
		ctx.Redirect(http.StatusFound, inboxReturnURL(folder, contactID, "error", err.Error()))
		return
	}
	ctx.Redirect(http.StatusFound, inboxReturnURL(folder, contactID, "success", "Reply sent"))
}

// InterestedContactsRedirect sends the old Interested page to the inbox folder.
func InterestedContactsRedirect(ctx *gin.Context) {
	q := url.Values{}
	q.Set("folder", model.InboxFolderInterested)
	if c := strings.TrimSpace(ctx.Query("campaign")); c != "" {
		q.Set("campaign", c)
	}
	if s := strings.TrimSpace(ctx.Query("success")); s != "" {
		q.Set("success", s)
	}
	if e := strings.TrimSpace(ctx.Query("error")); e != "" {
		q.Set("error", e)
	}
	ctx.Redirect(http.StatusFound, "/inbox?"+q.Encode())
}

// SendsPageRedirect keeps old /sends URLs working.
func SendsPageRedirect(ctx *gin.Context) {
	q := url.Values{}
	q.Set("folder", model.InboxFolderSent)
	if s := strings.TrimSpace(ctx.Query("status")); s != "" {
		q.Set("status", s)
	}
	if s := strings.TrimSpace(ctx.Query("q")); s != "" {
		q.Set("q", s)
	}
	if s := strings.TrimSpace(ctx.Query("success")); s != "" {
		q.Set("success", s)
	}
	if e := strings.TrimSpace(ctx.Query("error")); e != "" {
		q.Set("error", e)
	}
	ctx.Redirect(http.StatusFound, "/inbox?"+q.Encode())
}

func NewSendRedirect(ctx *gin.Context) {
	q := url.Values{}
	if c := strings.TrimSpace(ctx.Query("contact_id")); c != "" {
		q.Set("contact_id", c)
	}
	if m := strings.TrimSpace(ctx.Query("smtp_account_id")); m != "" {
		q.Set("smtp_account_id", m)
	}
	if e := strings.TrimSpace(ctx.Query("error")); e != "" {
		q.Set("error", e)
	}
	dest := "/inbox/compose"
	if enc := q.Encode(); enc != "" {
		dest += "?" + enc
	}
	ctx.Redirect(http.StatusFound, dest)
}

func normalizeComposeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", strconv.ErrSyntax
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

type mailboxOption struct {
	ID    int64
	Label string
}

func inboxMailboxOptions(userID int64) ([]mailboxOption, int64) {
	mailboxes, _ := model.ListSendReadyAccountsForUser(userID)
	var opts []mailboxOption
	defaultID := int64(0)
	for _, acc := range mailboxes {
		email := acc.SenderEmail()
		label := email
		if fn := strings.TrimSpace(acc.FromName); fn != "" {
			label = fn + " <" + email + ">"
		}
		opts = append(opts, mailboxOption{ID: acc.ID, Label: label})
		if acc.IsDefault && defaultID == 0 {
			defaultID = acc.ID
		}
	}
	if defaultID == 0 && len(opts) > 0 {
		defaultID = opts[0].ID
	}
	return opts, defaultID
}

func InboxComposePage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templates, _ := model.ListTemplates(userID)
	contacts, _ := model.ListContacts(userID)
	preselectedContactID, _ := strconv.ParseInt(ctx.Query("contact_id"), 10, 64)
	preselectedMailboxID, _ := strconv.ParseInt(ctx.Query("smtp_account_id"), 10, 64)
	preselectedEmail := strings.TrimSpace(ctx.Query("email"))
	if preselectedContactID > 0 && preselectedEmail == "" {
		if c, _, err := model.GetContactForUser(preselectedContactID, userID); err == nil {
			preselectedEmail = c.Email
		}
	}
	mailboxOpts, defaultMailboxID := inboxMailboxOptions(userID)
	if preselectedMailboxID == 0 {
		preselectedMailboxID = defaultMailboxID
	}
	verifyStatus, verifyReason := "", ""
	if preselectedContactID > 0 {
		verifyStatus, verifyReason, _ = model.GetContactEmailStatus(preselectedContactID)
	}

	ctx.HTML(http.StatusOK, "inbox_compose.html", gin.H{
		"title":                "New email",
		"active":               "inbox",
		"templates":            templates,
		"contacts":             contacts,
		"mailboxes":            mailboxOpts,
		"preselectedContactID": preselectedContactID,
		"preselectedMailboxID": preselectedMailboxID,
		"preselectedEmail":     preselectedEmail,
		"verifyStatus":         verifyStatus,
		"verifyReason":         verifyReason,
		"gmailSendBlocked":     model.GmailSendBlocked(userID),
		"inboxUnread":          model.CountInboxUnread(userID),
		"error":                ctx.Query("error"),
		"success":              ctx.Query("success"),
	})
}

func InboxComposeVerify(ctx *gin.Context) {
	userID := mustUserID(ctx)
	email := strings.TrimSpace(ctx.PostForm("email"))
	contactID, _ := strconv.ParseInt(ctx.PostForm("contact_id"), 10, 64)
	if email == "" && contactID == 0 {
		var req struct {
			Email     string `json:"email"`
			ContactID int64  `json:"contact_id"`
		}
		_ = ctx.ShouldBindJSON(&req)
		email = strings.TrimSpace(req.Email)
		contactID = req.ContactID
	}
	if contactID <= 0 {
		parsed, err := normalizeComposeEmail(email)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Enter a valid email address"})
			return
		}
		id, err := model.FindOrCreateContact(userID, parsed, nil)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Could not save contact"})
			return
		}
		contactID = id
		email = parsed
	} else {
		c, _, err := model.GetContactForUser(contactID, userID)
		if err != nil {
			ctx.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "Contact not found"})
			return
		}
		email = c.Email
	}

	sum, err := model.VerifyContactsForUser(userID, []int64{contactID}, false, emailVerifyFn)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "contact_id": contactID, "email": email})
		return
	}
	status, reason, _ := model.GetContactEmailStatus(contactID)
	ctx.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"contact_id": contactID,
		"email":      email,
		"status":     status,
		"reason":     reason,
		"summary":    sum.FlashMessage(),
		"can_send":   status != "invalid",
	})
}

func InboxComposeSend(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templateID, err := strconv.ParseInt(ctx.PostForm("template_id"), 10, 64)
	if err != nil || templateID <= 0 {
		ctx.Redirect(http.StatusFound, "/inbox/compose?error="+url.QueryEscape("Pick a template"))
		return
	}
	smtpAccountID, _ := strconv.ParseInt(ctx.PostForm("smtp_account_id"), 10, 64)
	if smtpAccountID <= 0 {
		ctx.Redirect(http.StatusFound, "/inbox/compose?error="+url.QueryEscape("Pick a mailbox to send from"))
		return
	}

	contactID, _ := strconv.ParseInt(ctx.PostForm("contact_id"), 10, 64)
	emailRaw := strings.TrimSpace(ctx.PostForm("email"))
	if contactID <= 0 {
		parsed, err := normalizeComposeEmail(emailRaw)
		if err != nil {
			ctx.Redirect(http.StatusFound, "/inbox/compose?error="+url.QueryEscape("Enter a valid email address"))
			return
		}
		contactID, err = model.FindOrCreateContact(userID, parsed, nil)
		if err != nil {
			ctx.Redirect(http.StatusFound, "/inbox/compose?error="+url.QueryEscape("Could not save contact"))
			return
		}
	} else if _, _, err := model.GetContactForUser(contactID, userID); err != nil {
		ctx.Redirect(http.StatusFound, "/inbox/compose?error="+url.QueryEscape("Contact not found"))
		return
	}

	if status, _, _ := model.GetContactEmailStatus(contactID); status == "invalid" {
		ctx.Redirect(http.StatusFound, "/inbox/compose?contact_id="+strconv.FormatInt(contactID, 10)+"&error="+url.QueryEscape("Email marked invalid — pick another address or re-verify"))
		return
	}

	emailSendID, err := processAndSendEmail(userID, templateID, contactID, 0, "", 0, smtpAccountID)
	if err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/inbox/compose?contact_id="+strconv.FormatInt(contactID, 10)+"&error="+url.QueryEscape(err.Error()))
		return
	}
	ctx.Redirect(http.StatusFound, "/inbox?folder=sent&send="+strconv.FormatInt(emailSendID, 10)+"&contact="+strconv.FormatInt(contactID, 10)+"&success="+url.QueryEscape("Email queued for delivery"))
}
