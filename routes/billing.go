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

	onPro := currentTier == model.PlanTierPro && !model.UserIsAdmin(user)
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
		"onPro":             onPro,
		"hasPaidMembership": onPro && user.WhopMembershipID != "",
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
	if model.UserIsAdmin(user) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Admin accounts keep full access"))
		return
	}
	if model.NormalizePlanTier(user.PlanTier) == model.PlanTierFree {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("You are already on Free"))
		return
	}

	mode := strings.TrimSpace(strings.ToLower(c.PostForm("mode")))
	if mode != "immediate" {
		mode = "at_period_end"
	}

	// Local Pro without a Whop membership (manual / webhook gap): apply Free immediately.
	if user.WhopMembershipID == "" {
		if err := downgradeUserToFree(userID); err != nil {
			log.Printf("billing downgrade local: %v", err)
			c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape(err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Switched to Free plan."))
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
		if err := downgradeUserToFree(userID); err != nil {
			log.Printf("billing downgrade after immediate cancel: %v", err)
			c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Whop cancelled, but local Free switch failed: "+err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Pro revoked. You are now on the Free plan."))
		return
	}

	// Period-end: keep Pro until renewal; refresh access-until date for the UI.
	if mem, err := whop.GetMembership(user.WhopMembershipID); err == nil && mem.RenewalPeriodEnd != nil {
		_ = model.UpdateUserSubscription(userID, user.SubscriptionStatus, user.WhopMembershipID, user.WhopMemberID, mem.RenewalPeriodEnd)
	}

	c.Redirect(http.StatusFound, "/settings/billing?success="+url.QueryEscape("Subscription will cancel at the end of your billing period. You keep Pro until then, then move to Free."))
}

func downgradeUserToFree(userID int64) error {
	if err := model.ApplyPlanLimitsToUser(userID, model.PlanTierFree); err != nil {
		return err
	}
	// Free users keep app access with an active free entitlement (no Whop membership).
	return model.UpdateUserSubscription(userID, model.SubStatusActive, "", "", nil)
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
		_ = model.ApplyPlanLimitsToUser(userID, model.PlanTierFree)
		_ = model.UpdateUserSubscription(userID, model.SubStatusActive, "", "", nil)
		return
	}
	if m.User != nil && m.User.Email != "" {
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.ApplyPlanLimitsToUser(u.ID, model.PlanTierFree)
			_ = model.UpdateUserSubscription(u.ID, model.SubStatusActive, "", "", nil)
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
		_ = model.ApplyPlanLimitsToUser(userID, model.PlanTierFree)
		_ = model.UpdateUserSubscription(userID, model.SubStatusActive, "", "", nil)
		return
	}
	if m.User != nil && m.User.Email != "" {
		if u, err := model.GetUserByEmail(m.User.Email); err == nil {
			_ = model.ApplyPlanLimitsToUser(u.ID, model.PlanTierFree)
			_ = model.UpdateUserSubscription(u.ID, model.SubStatusActive, "", "", nil)
		}
	}
}

func planTierFromMetadata(meta map[string]interface{}) model.PlanTier {
	if meta == nil {
		return model.PlanTierPro
	}
	if v, ok := meta["plan_tier"].(string); ok {
		return model.NormalizePlanTier(v)
	}
	return model.PlanTierPro
}
