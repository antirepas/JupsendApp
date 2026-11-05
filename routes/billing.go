package routes

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/model"
	"emailtracker.com/whop"
	"github.com/gin-gonic/gin"
)

type billingPlanView struct {
	Tier            string
	Name            string
	Label           string
	MonthlyEmails   int
	MaxContacts     int
	AICredits       int
	MonthlyPriceUSD int
	Recommended     bool
	Current         bool
	CheckoutLabel   string
}

func BillingPage(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	currentTier := model.NormalizePlanTier(user.PlanTier)
	currentPlan := model.PlanInfoForTier(currentTier)

	var plans []billingPlanView
	for _, p := range model.PaidPlanTiers() {
		checkout := "Choose " + p.Name
		if p.Tier == currentTier {
			checkout = "Current plan"
		} else if model.IsPaidPlanTier(currentTier) {
			if planRank(p.Tier) > planRank(currentTier) {
				checkout = "Upgrade"
			} else {
				checkout = "Switch"
			}
		}
		plans = append(plans, billingPlanView{
			Tier:            string(p.Tier),
			Name:            p.Name,
			Label:           p.Label,
			MonthlyEmails:   p.MonthlyEmails,
			MaxContacts:     p.MaxContacts,
			AICredits:       p.AICredits,
			MonthlyPriceUSD: p.MonthlyPriceUSD,
			Recommended:     p.Recommended,
			Current:         p.Tier == currentTier && model.UserHasActiveSubscription(user),
			CheckoutLabel:   checkout,
		})
	}

	statusLabel := subscriptionStatusLabel(user)
	renewalDate := ""
	cancelAtPeriodEnd := false
	manageURL := ""

	if user.WhopMembershipID != "" {
		if mem, err := whop.GetMembership(user.WhopMembershipID); err == nil {
			manageURL = mem.ManageURL
			cancelAtPeriodEnd = mem.CancelAtPeriodEnd
			if mem.RenewalPeriodEnd != nil {
				renewalDate = mem.RenewalPeriodEnd.Format("Jan 2, 2006")
			}
		}
	}
	if renewalDate == "" && user.SubscriptionEndsAt != nil {
		renewalDate = user.SubscriptionEndsAt.Format("Jan 2, 2006")
	}

	successMsg := c.Query("success")
	if successMsg == "1" {
		successMsg = "Subscription updated. You can now use the full app."
	}

	onPaid := model.IsPaidPlanTier(currentTier) && !model.UserIsAdmin(user)
	emailsUsed := 0
	emailsCap := currentPlan.MonthlyEmails
	emailsPct := 0.0
	contactsUsed := 0
	contactsCap := currentPlan.MaxContacts
	contactsPct := 0.0
	if onPaid || model.UserIsAdmin(user) {
		if n, err := model.CountUserEmailsThisMonth(userID); err == nil {
			emailsUsed = n
		}
		if n, err := model.CountUserContacts(userID); err == nil {
			contactsUsed = n
		}
		if emailsCap > 0 {
			emailsPct = float64(emailsUsed) / float64(emailsCap) * 100
			if emailsPct > 100 {
				emailsPct = 100
			}
		}
		if contactsCap > 0 {
			contactsPct = float64(contactsUsed) / float64(contactsCap) * 100
			if contactsPct > 100 {
				contactsPct = 100
			}
		}
	}

	c.HTML(http.StatusOK, "billing.html", gin.H{
		"title":             "Billing",
		"active":            "billing",
		"user":              user,
		"subscribed":        model.UserHasAppAccess(user),
		"isAdmin":           model.UserIsAdmin(user),
		"success":           successMsg,
		"error":             c.Query("error"),
		"currentPlan":       currentPlan,
		"plans":             plans,
		"statusLabel":       statusLabel,
		"renewalDate":       renewalDate,
		"cancelAtPeriodEnd": cancelAtPeriodEnd,
		"manageURL":         manageURL,
		"onPaid":            onPaid,
		"hasPaidMembership": onPaid && user.WhopMembershipID != "",
		"whopConfigured":    whop.IsConfigured(),
		"emailsUsed":        emailsUsed,
		"emailsCap":         emailsCap,
		"emailsPct":         emailsPct,
		"contactsUsed":      contactsUsed,
		"contactsCap":       contactsCap,
		"contactsPct":       contactsPct,
	})
}

func planRank(tier model.PlanTier) int {
	switch model.NormalizePlanTier(string(tier)) {
	case model.PlanTierStarter:
		return 1
	case model.PlanTierGrowth:
		return 2
	case model.PlanTierScale:
		return 3
	case model.PlanTierHyper:
		return 4
	default:
		return 0
	}
}

func subscriptionStatusLabel(u model.User) string {
	if model.UserIsAdmin(u) {
		return "Admin"
	}
	switch u.SubscriptionStatus {
	case model.SubStatusActive:
		if model.IsPaidPlanTier(model.NormalizePlanTier(u.PlanTier)) {
			return "Active"
		}
		return "Inactive"
	case model.SubStatusPendingPayment:
		return "Pending payment"
	case model.SubStatusPastDue:
		return "Past due"
	case model.SubStatusCancelled:
		return "Cancelled"
	default:
		return "Inactive"
	}
}

func BillingCheckout(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	raw := strings.TrimSpace(c.PostForm("plan"))
	if !model.IsValidSignupPlan(raw) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Pick a plan to continue"))
		return
	}
	tier := model.NormalizePlanTier(raw)

	current := model.NormalizePlanTier(user.PlanTier)
	if tier == current && model.UserHasActiveSubscription(user) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("You are already on the "+model.PlanInfoForTier(tier).Name+" plan"))
		return
	}

	base := model.UserBaseURL(userID)
	redirectURL := strings.TrimRight(base, "/") + "/settings/billing?success=1"
	purchaseURL, err := whop.CreateCheckout(userID, tier, redirectURL)
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

func BillingCancel(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	if model.UserIsAdmin(user) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Admin accounts keep full access"))
		return
	}
	if !model.UserHasAppAccess(user) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("No active subscription to cancel"))
		return
	}

	mode := strings.TrimSpace(strings.ToLower(c.PostForm("mode")))
	if mode != "immediate" {
		mode = "at_period_end"
	}

	// Local paid without a Whop membership (manual / webhook gap): revoke immediately.
	if user.WhopMembershipID == "" {
		if err := model.RevokePaidAccess(userID); err != nil {
			log.Printf("billing downgrade local: %v", err)
			c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape(err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Subscription ended."))
		return
	}

	if err := whop.CancelMembership(user.WhopMembershipID, mode); err != nil {
		log.Printf("whop cancel: %v", err)
		msg := err.Error()
		if len(msg) > 180 {
			msg = msg[:180]
		}
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape(msg))
		return
	}

	if mode == "immediate" {
		if err := model.RevokePaidAccess(userID); err != nil {
			log.Printf("billing downgrade after immediate cancel: %v", err)
			c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Whop cancelled, but local revoke failed: "+err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Subscription ended."))
		return
	}

	// Period-end: keep paid plan until renewal; refresh access-until date for the UI.
	if mem, err := whop.GetMembership(user.WhopMembershipID); err == nil && mem.RenewalPeriodEnd != nil {
		_ = model.UpdateUserSubscription(userID, user.SubscriptionStatus, user.WhopMembershipID, user.WhopMemberID, mem.RenewalPeriodEnd)
	}

	c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Subscription will cancel at the end of your billing period. You keep access until then."))
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
	case "payment.succeeded", "payment.created":
		handlePaymentSucceeded(evt.Data)
	case "payment.failed":
		handlePaymentFailed(evt.Data)
	}
	c.Status(http.StatusOK)
}

func handlePaymentSucceeded(data json.RawMessage) {
	var payload struct {
		Metadata map[string]interface{} `json:"metadata"`
		User     *struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	if isMailboxAddonMetadata(payload.Metadata) || isDomainAddonMetadata(payload.Metadata) {
		fulfillMailboxPurchaseFromMetadata(payload.Metadata)
	}
}

func handleMembershipActivated(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	if isMailboxAddonMetadata(m.Metadata) || isDomainAddonMetadata(m.Metadata) {
		fulfillMailboxPurchaseFromMetadata(m.Metadata)
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	tier := planTierFromMembership(m)
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
		_ = model.ApplyPlanLimitsToUser(userID, tier)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusActive, m.ID, memberID, ends)
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.ApplyPlanLimitsToUser(u.ID, tier)
		}
	}
}

func isMailboxAddonMetadata(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	purpose, _ := meta["purpose"].(string)
	return purpose == "mailbox_addon"
}

func isDomainAddonMetadata(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	purpose, _ := meta["purpose"].(string)
	return purpose == "domain_addon"
}

func fulfillMailboxPurchaseFromMetadata(meta map[string]interface{}) {
	if isDomainAddonMetadata(meta) {
		var purchaseID int64
		switch v := meta["domain_purchase_id"].(type) {
		case string:
			purchaseID, _ = strconv.ParseInt(v, 10, 64)
		case float64:
			purchaseID = int64(v)
		}
		if purchaseID > 0 {
			if err := FulfillDomainPurchase(purchaseID); err != nil {
				log.Printf("domain purchase fulfill %d: %v", purchaseID, err)
			}
		}
		return
	}
	if !isMailboxAddonMetadata(meta) {
		return
	}
	var purchaseID int64
	switch v := meta["mailbox_purchase_id"].(type) {
	case string:
		purchaseID, _ = strconv.ParseInt(v, 10, 64)
	case float64:
		purchaseID = int64(v)
	}
	if purchaseID <= 0 {
		return
	}
	if err := FulfillMailboxPurchase(purchaseID); err != nil {
		log.Printf("mailbox purchase fulfill %d: %v", purchaseID, err)
	}
}

func handleMembershipDeactivated(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	if userID > 0 {
		_ = model.RevokePaidAccess(userID)
		return
	}
	if m.User != nil && m.User.Email != "" {
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.RevokePaidAccess(u.ID)
		}
	}
}

func handlePaymentFailed(data json.RawMessage) {
	m, err := whop.ParseMembershipActivated(data)
	if err != nil {
		return
	}
	userID := whop.UserIDFromMetadata(m.Metadata)
	if userID > 0 {
		_ = model.RevokePaidAccess(userID)
		return
	}
	if m.User != nil && m.User.Email != "" {
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.RevokePaidAccess(u.ID)
		}
	}
}

func planTierFromMembership(m whop.MembershipData) model.PlanTier {
	if tier := planTierFromMetadata(m.Metadata); model.IsPaidPlanTier(tier) {
		return tier
	}
	return model.PlanTierStarter
}

func planTierFromMetadata(meta map[string]interface{}) model.PlanTier {
	if meta == nil {
		return model.PlanTierStarter
	}
	if v, ok := meta["plan_tier"].(string); ok && strings.TrimSpace(v) != "" {
		return model.NormalizePlanTier(v)
	}
	if v, ok := meta["plan_id"].(string); ok {
		return whop.TierFromWhopPlanID(v)
	}
	return model.PlanTierStarter
}
