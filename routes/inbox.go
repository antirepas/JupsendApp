package routes

import (
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

func inboxFolderCounts(userID int64) map[string]int {
	counts := map[string]int{
		model.InboxFolderAll:        0,
		model.InboxFolderUnread:     0,
		model.InboxFolderInterested: 0,
		model.InboxFolderOpened:     0,
		model.InboxFolderClicked:    0,
	}
	for _, folder := range []string{
		model.InboxFolderAll,
		model.InboxFolderUnread,
		model.InboxFolderInterested,
		model.InboxFolderOpened,
		model.InboxFolderClicked,
	} {
		list, err := model.ListInboxThreads(userID, folder, "", 500)
		if err != nil {
			continue
		}
		counts[folder] = len(list)
	}
	return counts
}

func InboxPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	folder := model.NormalizeInboxFolder(ctx.Query("folder"))
	q := strings.TrimSpace(ctx.Query("q"))
	contactID, _ := strconv.ParseInt(ctx.Query("contact"), 10, 64)

	threads, err := model.ListInboxThreads(userID, folder, q, 200)
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"title": "Error", "active": "inbox", "error": "Failed to load inbox",
		})
		return
	}

	var (
		selected     *model.InboxThreadDetail
		selectedID   int64
		replySubject string
		replyFrom    string
	)
	if contactID > 0 {
		detail, err := model.GetInboxThread(userID, contactID)
		if err != nil {
			log.Print(err)
			ctx.Redirect(http.StatusFound, inboxReturnURL(folder, 0, "error", "Thread not found"))
			return
		}
		enrichLegacyConversationBodies(userID, contactID, detail.Messages)
		repairMangledConversationBodies(detail.Messages)
		selected = detail
		selectedID = contactID
		replySubject = detail.Subject
		if !strings.HasPrefix(strings.ToLower(replySubject), "re:") {
			replySubject = "Re: " + replySubject
		}
		if accID, err := model.LatestSMTPAccountForContact(userID, contactID); err == nil && accID > 0 {
			if acc, err := model.GetSMTPAccount(accID); err == nil {
				replyFrom = acc.SenderEmail()
			}
		}
		// Refresh thread unread flags after mark-read.
		for i := range threads {
			if threads[i].ContactID == contactID {
				threads[i].Unread = false
			}
		}
	}

	folderTitle := "Inbox"
	switch folder {
	case model.InboxFolderUnread:
		folderTitle = "Unread"
	case model.InboxFolderInterested:
		folderTitle = "Interested"
	case model.InboxFolderOpened:
		folderTitle = "Opened"
	case model.InboxFolderClicked:
		folderTitle = "Clicked"
	}

	ctx.HTML(http.StatusOK, "inbox.html", gin.H{
		"title":         "Inbox",
		"active":        "inbox",
		"folder":        folder,
		"folderTitle":   folderTitle,
		"q":             q,
		"threads":       threads,
		"folderCounts":  inboxFolderCounts(userID),
		"inboxUnread":   model.CountInboxUnread(userID),
		"selected":      selected,
		"selectedID":    selectedID,
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
