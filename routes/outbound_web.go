package routes

import (
	"net/http"
	"strconv"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

const suppressionsPath = "/contacts/suppressions"

func ListSuppressionsPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	items, err := model.ListSuppressions(userID)
	if err != nil {
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Failed to load suppressions"})
		return
	}
	contacts, _ := model.ListContacts(userID)
	ctx.HTML(http.StatusOK, "suppressions_list.html", gin.H{
		"title":        "Suppressions",
		"active":       "contacts",
		"suppressions": items,
		"contacts":     contacts,
		"success":      ctx.Query("success"),
		"error":        ctx.Query("error"),
	})
}

func RedirectSuppressions(ctx *gin.Context) {
	q := ctx.Request.URL.RawQuery
	dest := suppressionsPath
	if q != "" {
		dest += "?" + q
	}
	ctx.Redirect(http.StatusFound, dest)
}

func AddSuppressionWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.PostForm("contact_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, suppressionsPath+"?error=Invalid+contact")
		return
	}
	if _, _, err := model.GetContactForUser(contactID, userID); err != nil {
		ctx.Redirect(http.StatusFound, suppressionsPath+"?error=Contact+not+found")
		return
	}
	reason := ctx.PostForm("reason")
	if reason == "" {
		reason = "manual"
	}
	if err := model.SuppressContact(contactID, reason, "manual", 0); err != nil {
		ctx.Redirect(http.StatusFound, suppressionsPath+"?error=Failed+to+suppress")
		return
	}
	ctx.Redirect(http.StatusFound, suppressionsPath+"?success=Contact+suppressed")
}

func RemoveSuppressionWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contactID, err := strconv.ParseInt(ctx.Param("contact_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, suppressionsPath+"?error=Invalid+contact")
		return
	}
	if _, _, err := model.GetContactForUser(contactID, userID); err != nil {
		ctx.Redirect(http.StatusFound, suppressionsPath+"?error=Contact+not+found")
		return
	}
	_ = model.RemoveSuppression(contactID)
	ctx.Redirect(http.StatusFound, suppressionsPath+"?success=Suppression+removed")
}

func GetSendJobsAPI(ctx *gin.Context) {
	userID := mustUserID(ctx)
	campaignID, _ := strconv.ParseInt(ctx.Query("campaign_id"), 10, 64)
	if campaignID == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "campaign_id required"})
		return
	}
	if _, err := model.GetCampaignForUser(campaignID, userID); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"message": "campaign not found"})
		return
	}
	counts, err := model.CountSendJobsByCampaign(campaignID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, counts)
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
