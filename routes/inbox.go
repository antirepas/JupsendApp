package routes

import (
	"html"
	"log"
	"net/http"
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

	var (
		threads      []model.InboxThread
		threadsErr   error
		folderCounts model.InboxFolderCounts
		selected     *model.InboxThreadDetail
		selectedID   int64
		replySubject string
		replyFrom    string
		selectedErr  error
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		threads, threadsErr = model.ListInboxThreads(userID, folder, q, 75)
		folderCounts = model.CountInboxFolders(userID)
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

	if threadsErr != nil {
		log.Print(threadsErr)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"title": "Error", "active": "inbox", "error": "Failed to load inbox",
		})
		return
	}
	if contactID > 0 && selectedErr != nil {
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
		"title":        "Inbox",
		"active":       "inbox",
		"folder":       folder,
		"folderTitle":  inboxFolderTitle(folder),
		"q":            q,
		"threads":      threads,
		"folderCounts": folderCounts.Map(),
		"inboxUnread":  folderCounts.Unread,
		"selected":     selected,
		"selectedID":   selectedID,
		"replySubject": replySubject,
		"replyFrom":    replyFrom,
		"success":      ctx.Query("success"),
		"error":        ctx.Query("error"),
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
