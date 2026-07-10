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

type billingPlanView struct {
	Tier        string
	Name        string
	DailyEmails int
	AICredits   int
	Warmup      bool
	Current     bool
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
	for _, p := range model.AllPlanTiers() {
		plans = append(plans, billingPlanView{
			Tier:        string(p.Tier),
			Name:        p.Name,
			DailyEmails: p.DailyEmails,
			AICredits:   p.AICredits,
			Warmup:      p.Warmup,
			Current:     p.Tier == currentTier,
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
		"hasPaidMembership": currentTier != model.PlanTierFree && user.WhopMembershipID != "",
		"whopConfigured":    whop.IsConfigured(),
	})
}

func subscriptionStatusLabel(u model.User) string {
	if model.UserIsAdmin(u) {
		return "Admin"
	}
	switch u.SubscriptionStatus {
	case model.SubStatusActive:
		if model.NormalizePlanTier(u.PlanTier) == model.PlanTierFree {
			return "Free"
		}
		return "Active"
	case model.SubStatusPendingPayment:
		return "Pending payment"
	case model.SubStatusPastDue:
		return "Past due"
	case model.SubStatusCancelled:
		return "Cancelled"
	default:
		if model.NormalizePlanTier(u.PlanTier) == model.PlanTierFree {
			return "Free"
		}
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

	tier := model.NormalizePlanTier(c.PostForm("plan"))
	if tier == model.PlanTierFree {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Use cancel subscription to move to Free"))
		return
	}

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
	if user.WhopMembershipID == "" {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("No active Whop membership to cancel"))
		return
	}
	if err := whop.CancelMembership(user.WhopMembershipID); err != nil {
		log.Printf("whop cancel: %v", err)
		msg := err.Error()
		if len(msg) > 180 {
			msg = msg[:180]
		}
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape(msg))
		return
	}

	// Refresh renewal end from Whop so UI can show access-until date.
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
	tier := planTierFromMetadata(m.Metadata)
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
		_ = model.ApplyPlanLimitsToUser(userID, model.PlanTierFree)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusCancelled, m.ID, memberID, nil)
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.ApplyPlanLimitsToUser(u.ID, model.PlanTierFree)
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
		_ = model.UpdateUserSubscription(userID, model.SubStatusPastDue, m.ID, "", nil)
		_ = model.ApplyPlanLimitsToUser(userID, model.PlanTierFree)
		return
	}
	if m.User != nil && m.User.Email != "" {
		_ = model.UpdateUserSubscriptionByEmail(m.User.Email, model.SubStatusPastDue, m.ID, "", nil)
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.ApplyPlanLimitsToUser(u.ID, model.PlanTierFree)
		}
	}
}

func planTierFromMetadata(meta map[string]interface{}) model.PlanTier {
	if meta == nil {
		return model.PlanTierStandard
	}
	if v, ok := meta["plan_tier"].(string); ok {
		switch strings.ToLower(v) {
		case string(model.PlanTierFree):
			return model.PlanTierFree
		case string(model.PlanTierPro):
			return model.PlanTierPro
		default:
			return model.PlanTierStandard
		}
	}
	return model.PlanTierStandard
}
