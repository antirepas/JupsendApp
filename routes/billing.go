package routes

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/whop"
	"github.com/gin-gonic/gin"
)

func BillingPage(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	c.HTML(http.StatusOK, "billing.html", gin.H{
		"title":      "Billing",
		"active":     "billing",
		"user":       user,
		"subscribed": model.UserHasAppAccess(user),
		"isAdmin":    model.UserIsAdmin(user),
		"success":    c.Query("success"),
		"error":      c.Query("error"),
	})
}

func BillingCheckout(c *gin.Context) {
	userID := mustUserID(c)
	if _, err := model.GetUserByID(userID); err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	base := model.UserBaseURL(userID)
	redirectURL := strings.TrimRight(base, "/") + "/settings/billing?success=1"
	purchaseURL, err := whop.CreateCheckout(userID, redirectURL)
	if err != nil {
		log.Printf("whop checkout: %v", err)
		msg := err.Error()
		if len(msg) > 180 {
			msg = msg[:180]
		}
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape(msg))
		return
	}
	c.Redirect(http.StatusFound, purchaseURL)
}

func WhopWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if !whop.VerifyWebhookSignature(body, c.Request.Header) {
		c.Status(http.StatusUnauthorized)
		return
	}
	var evt whop.WebhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	switch evt.Type {
	case "membership.activated":
		handleMembershipActivated(evt.Data)
	case "membership.deactivated":
		handleMembershipDeactivated(evt.Data)
	case "payment.failed":
		handlePaymentFailed(evt.Data)
	}
	c.Status(http.StatusOK)
}

func handleMembershipActivated(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	memberID := ""
	if m.Member != nil {
		memberID = m.Member.ID
	}
	var ends *time.Time
	if m.RenewalPeriodEnd != nil {
		ends = m.RenewalPeriodEnd
	}
	if userID > 0 {
		_ = model.UpdateUserSubscription(userID, model.SubStatusActive, m.ID, memberID, ends)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusActive, m.ID, memberID, ends)
	}
}

func handleMembershipDeactivated(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	memberID := ""
	if m.Member != nil {
		memberID = m.Member.ID
	}
	if userID > 0 {
		_ = model.UpdateUserSubscription(userID, model.SubStatusCancelled, m.ID, memberID, nil)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusCancelled, m.ID, memberID, nil)
	}
}

func handlePaymentFailed(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	if userID > 0 {
		_ = model.UpdateUserSubscription(userID, model.SubStatusPastDue, m.ID, "", nil)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusPastDue, m.ID, "", nil)
	}
}
