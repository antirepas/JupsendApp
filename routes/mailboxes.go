package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/inboxkit"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/whop"
	"github.com/gin-gonic/gin"
)

func RequireMailboxSetup() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := mustUserID(c)

		// Free: ensure shared SMTP is attached, then allow app use without domain onboarding.
		// Do not call ApplyPlanLimitsToUser on every request — it used to reset sends_today.
		if !model.UserIsPro(userID) {
			_ = model.EnsureFreeSharedMailbox(userID)
			if model.UserHasReadyMailbox(userID) {
				c.Next()
				return
			}
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/settings") ||
				strings.HasPrefix(path, "/mailboxes") ||
				strings.HasPrefix(path, "/guides") ||
				strings.HasPrefix(path, "/onboarding/") {
				c.Next()
				return
			}
			c.Redirect(http.StatusFound, "/settings?error="+url.QueryEscape("Shared sending mailbox is not configured on this server. Contact support or check SMTP_* env vars."))
			c.Abort()
			return
		}

		if model.UserHasReadyMailbox(userID) {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/onboarding/") ||
			strings.HasPrefix(path, "/mailboxes") ||
			strings.HasPrefix(path, "/settings") ||
			strings.HasPrefix(path, "/guides") {
			c.Next()
			return
		}
		c.Redirect(http.StatusFound, "/onboarding/domain")
		c.Abort()
	}
}

func OnboardingDomainPage(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	if model.UserHasReadyMailbox(userID) {
		c.Redirect(http.StatusFound, "/mailboxes")
		return
	}
	user, _ := model.GetUserByID(userID)
	domains, _ := model.ListOutreachDomains(userID)
	var pending *model.OutreachDomain
	for i := range domains {
		if domains[i].Status != "ready" && domains[i].Status != "error" {
			pending = &domains[i]
			break
		}
		if domains[i].Status == "ready" && !model.UserHasReadyMailbox(userID) {
			pending = &domains[i]
			break
		}
	}
	c.HTML(http.StatusOK, "onboarding_domain.html", gin.H{
		"title":            "Set up outreach domain",
		"active":           "mailboxes",
		"user":             user,
		"inboxkitOK":       inboxkit.Configured(),
		"includedCount":    config.InboxKitIncludedMailboxCount(),
		"pendingDomain":    pending,
		"error":            c.Query("error"),
		"success":          c.Query("success"),
		"query":            c.Query("q"),
	})
}

func OnboardingDomainSearch(c *gin.Context) {
	if !model.UserIsPro(mustUserID(c)) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	q := strings.TrimSpace(c.PostForm("q"))
	if q == "" {
		q = strings.TrimSpace(c.Query("q"))
	}
	if q == "" {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape("Enter a company or keyword"))
		return
	}
	if !inboxkit.Configured() {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape("InboxKit is not configured on this server"))
		return
	}
	client := inboxkit.NewClient()
	results, err := client.SearchDomains(q)
	if err != nil {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape(err.Error())+"&q="+url.QueryEscape(q))
		return
	}
	user, _ := model.GetUserByID(mustUserID(c))
	c.HTML(http.StatusOK, "onboarding_domain.html", gin.H{
		"title":         "Set up outreach domain",
		"active":        "mailboxes",
		"user":          user,
		"inboxkitOK":    true,
		"includedCount": config.InboxKitIncludedMailboxCount(),
		"query":         q,
		"results":       results,
	})
}

func OnboardingDomainPurchase(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	domain := strings.ToLower(strings.TrimSpace(c.PostForm("domain")))
	if domain == "" || !strings.Contains(domain, ".") {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape("Pick a valid domain"))
		return
	}
	n := config.InboxKitIncludedMailboxCount()
	var specs []model.StarterMailboxSpec
	for i := 1; i <= n; i++ {
		fn := strings.TrimSpace(c.PostForm(fmt.Sprintf("first_name_%d", i)))
		ln := strings.TrimSpace(c.PostForm(fmt.Sprintf("last_name_%d", i)))
		local := strings.TrimSpace(c.PostForm(fmt.Sprintf("local_%d", i)))
		if fn == "" && ln == "" && local == "" {
			continue
		}
		if fn == "" {
			fn = "Team"
		}
		if ln == "" {
			ln = fmt.Sprintf("%d", i)
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	if len(specs) == 0 {
		user, _ := model.GetUserByID(userID)
		base := strings.Split(user.Email, "@")[0]
		specs = []model.StarterMailboxSpec{
			{FirstName: "Alex", LastName: "Outreach", LocalPart: "alex"},
			{FirstName: "Sam", LastName: "Outreach", LocalPart: "sam"},
			{FirstName: "Jordan", LastName: "Outreach", LocalPart: "jordan"},
		}
		if base != "" {
			specs[0].LocalPart = base
			specs[0].FirstName = base
		}
		if len(specs) > n {
			specs = specs[:n]
		}
	}
	domainID, orderID, err := model.PlaceStarterDomainOrder(userID, domain, specs)
	if err != nil {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape(err.Error()))
		return
	}
	_ = orderID
	c.Redirect(http.StatusFound, "/onboarding/domain/status?domain_id="+strconv.FormatInt(domainID, 10))
}

func OnboardingDomainStatus(c *gin.Context) {
	userID := mustUserID(c)
	domainID, _ := strconv.ParseInt(c.Query("domain_id"), 10, 64)
	d, err := model.GetOutreachDomain(domainID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/onboarding/domain?error="+url.QueryEscape("Domain setup not found"))
		return
	}
	_ = model.SyncInboxKitOrder(userID, domainID)
	d, _ = model.GetOutreachDomain(domainID, userID)
	if d.Status == "ready" && model.UserHasReadyMailbox(userID) {
		c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailboxes ready — you can start sending"))
		return
	}
	mailboxes, _ := model.ListOutreachMailboxes(userID)
	c.HTML(http.StatusOK, "onboarding_domain_status.html", gin.H{
		"title":     "Setting up domain",
		"active":    "mailboxes",
		"domain":    d,
		"mailboxes": mailboxes,
		"error":     c.Query("error"),
	})
}

func MailboxesPage(c *gin.Context) {
	userID := mustUserID(c)
	if pid, _ := strconv.ParseInt(c.Query("purchase_id"), 10, 64); pid > 0 {
		if p, err := model.GetMailboxPurchase(pid); err == nil && p.UserID == userID {
			if err := FulfillMailboxPurchase(pid); err != nil {
				log.Printf("mailbox purchase sync %d: %v", pid, err)
			}
		}
	}
	if pid, _ := strconv.ParseInt(c.Query("domain_purchase_id"), 10, 64); pid > 0 {
		if p, err := model.GetMailboxPurchase(pid); err == nil && p.UserID == userID {
			if err := FulfillDomainPurchase(pid); err != nil {
				log.Printf("domain purchase sync %d: %v", pid, err)
			}
		}
	}
	domains, _ := model.ListOutreachDomains(userID)
	mailboxes, _ := model.ListOutreachMailboxes(userID)
	for _, m := range mailboxes {
		if m.Status == "ready" && m.DomainID > 0 {
			_ = model.SyncInboxKitOrder(userID, m.DomainID)
			break
		}
	}
	mailboxes, _ = model.ListOutreachMailboxes(userID)
	user, _ := model.GetUserByID(userID)
	isPro := model.UserIsPro(userID)
	sharedReady := false
	sharedEmail := ""
	if !isPro {
		if acc, err := model.GetSMTPAccountByUserID(userID); err == nil && acc.MailboxSource == model.MailboxSourceShared {
			sharedReady = acc.IsSendReady()
			sharedEmail = acc.SenderEmail()
		}
	}

	type mailboxRow struct {
		ID           int64
		Email        string
		Status       string
		IsDefault    bool
		WarmupLabel  string
		InsightsHint string
	}
	var rows []mailboxRow
	for _, m := range mailboxes {
		row := mailboxRow{ID: m.ID, Email: m.Email, Status: m.Status, IsDefault: m.IsDefault}
		if m.SMTPAccountID > 0 {
			if acc, err := model.GetSMTPAccount(m.SMTPAccountID); err == nil {
				schedule := outbound.EffectiveDailyCap(acc)
				cap, hint := outbound.ApplyInsightsToCap(schedule, acc, m.AnalyticsJSON)
				if acc.WarmupEnabled {
					row.WarmupLabel = fmt.Sprintf("%d/%d today", acc.SendsToday, cap)
				} else {
					row.WarmupLabel = fmt.Sprintf("cap %d", acc.DailyLimit)
				}
				if hint.Adjusted {
					row.InsightsHint = hint.Reason
				} else if m.AnalyticsJSON != "" && m.AnalyticsJSON != "{}" {
					row.InsightsHint = "Schedule warmup"
				}
			}
		}
		rows = append(rows, row)
	}

	c.HTML(http.StatusOK, "mailboxes.html", gin.H{
		"title":         "Mailboxes",
		"active":        "mailboxes",
		"user":          user,
		"domains":       domains,
		"mailboxes":     mailboxes,
		"mailboxRows":   rows,
		"isPro":         isPro,
		"sharedReady":   sharedReady,
		"sharedEmail":   sharedEmail,
		"inboxkitOK":    inboxkit.Configured(),
		"whopAddon":     config.WhopMailboxAddonID != "" && whop.IsConfigured(),
		"success":       c.Query("success"),
		"error":         c.Query("error"),
		"includedCount": config.InboxKitIncludedMailboxCount(),
	})
}

func MailboxesSetDefault(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.SetDefaultOutreachMailbox(userID, id); err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Default mailbox updated"))
}

func MailboxesBuyPage(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Buying mailboxes requires Pro"))
		return
	}
	domains, _ := model.ListOutreachDomains(userID)
	if len(domains) == 0 {
		c.Redirect(http.StatusFound, "/onboarding/domain")
		return
	}
	c.HTML(http.StatusOK, "mailboxes_buy.html", gin.H{
		"title":   "Buy mailboxes",
		"active":  "mailboxes",
		"domains": domains,
		"error":   c.Query("error"),
		"whopOK":  config.WhopMailboxAddonID != "" && whop.IsConfigured(),
	})
}

func MailboxesBuyCheckout(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Buying mailboxes requires Pro"))
		return
	}
	domainID, _ := strconv.ParseInt(c.PostForm("domain_id"), 10, 64)
	qty, _ := strconv.Atoi(c.PostForm("quantity"))
	if qty < 1 {
		qty = 1
	}
	if qty > 50 {
		qty = 50
	}
	d, err := model.GetOutreachDomain(domainID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape("Pick a domain"))
		return
	}
	var specs []model.StarterMailboxSpec
	for i := 1; i <= qty; i++ {
		fn := strings.TrimSpace(c.PostForm(fmt.Sprintf("first_name_%d", i)))
		ln := strings.TrimSpace(c.PostForm(fmt.Sprintf("last_name_%d", i)))
		local := strings.TrimSpace(c.PostForm(fmt.Sprintf("local_%d", i)))
		if fn == "" {
			fn = "Seat"
		}
		if ln == "" {
			ln = fmt.Sprintf("%d", i)
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	payload, _ := json.Marshal(specs)
	purchaseID, err := model.CreateMailboxPurchase(userID, d.ID, qty, string(payload))
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape(err.Error()))
		return
	}
	if config.WhopMailboxAddonID == "" || !whop.IsConfigured() {
		// Dev/fallback: provision immediately without Whop when addon not configured.
		orderID, pErr := model.PlaceExtraMailboxesOrder(userID, d.ID, specs)
		if pErr != nil {
			_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", pErr.Error())
			c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(pErr.Error()))
			return
		}
		_ = model.UpdateMailboxPurchase(purchaseID, "provisioning", "", orderID, "")
		_ = model.SyncInboxKitOrder(userID, d.ID)
		c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailboxes ordered"))
		return
	}

	returnURL := strings.TrimRight(config.BaseURL, "/") + "/mailboxes?purchase_id=" + strconv.FormatInt(purchaseID, 10)
	checkoutURL, err := whop.CreateCheckoutWithPlanID(userID, config.WhopMailboxAddonID, returnURL, map[string]string{
		"user_id":             strconv.FormatInt(userID, 10),
		"mailbox_purchase_id": strconv.FormatInt(purchaseID, 10),
		"purpose":             "mailbox_addon",
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape(err.Error()))
		return
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "pending_payment", checkoutURL, "", "")
	c.Redirect(http.StatusFound, checkoutURL)
}

func FulfillMailboxPurchase(purchaseID int64) error {
	p, err := model.GetMailboxPurchase(purchaseID)
	if err != nil {
		return err
	}
	if p.Status == "fulfilled" || p.Status == "ready" {
		return nil
	}
	specs, err := model.ParseMailboxSpecsJSON(p.PayloadJSON)
	if err != nil || len(specs) == 0 {
		return fmt.Errorf("invalid mailbox purchase payload")
	}
	orderID, err := model.PlaceExtraMailboxesOrder(p.UserID, p.DomainID, specs)
	if err != nil {
		_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", err.Error())
		return err
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "provisioning", "", orderID, "")
	_ = model.SyncInboxKitOrder(p.UserID, p.DomainID)
	_ = model.UpdateMailboxPurchase(purchaseID, "fulfilled", "", orderID, "")
	return nil
}

func MailboxesBuyDomainPage(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Buying domains requires Pro"))
		return
	}
	c.HTML(http.StatusOK, "mailboxes_buy_domain.html", gin.H{
		"title":         "Buy domain",
		"active":        "mailboxes",
		"error":         c.Query("error"),
		"query":         c.Query("q"),
		"inboxkitOK":    inboxkit.Configured(),
		"whopOK":        config.WhopDomainAddonID != "" && whop.IsConfigured(),
		"includedCount": config.InboxKitIncludedMailboxCount(),
	})
}

func MailboxesBuyDomainSearch(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing")
		return
	}
	q := strings.TrimSpace(c.PostForm("q"))
	if q == "" {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape("Enter a keyword"))
		return
	}
	client := inboxkit.NewClient()
	results, err := client.SearchDomains(q)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape(err.Error())+"&q="+url.QueryEscape(q))
		return
	}
	c.HTML(http.StatusOK, "mailboxes_buy_domain.html", gin.H{
		"title":         "Buy domain",
		"active":        "mailboxes",
		"query":         q,
		"results":       results,
		"inboxkitOK":    true,
		"whopOK":        config.WhopDomainAddonID != "" && whop.IsConfigured(),
		"includedCount": config.InboxKitIncludedMailboxCount(),
	})
}

func MailboxesBuyDomainCheckout(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(c.PostForm("domain")))
	if domain == "" || !strings.Contains(domain, ".") {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape("Pick a valid domain"))
		return
	}
	n := config.InboxKitIncludedMailboxCount()
	var specs []model.StarterMailboxSpec
	for i := 1; i <= n; i++ {
		local := strings.TrimSpace(c.PostForm(fmt.Sprintf("local_%d", i)))
		fn := strings.TrimSpace(c.PostForm(fmt.Sprintf("first_name_%d", i)))
		ln := strings.TrimSpace(c.PostForm(fmt.Sprintf("last_name_%d", i)))
		if fn == "" {
			fn = "Team"
		}
		if ln == "" {
			ln = fmt.Sprintf("%d", i)
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	payload, _ := json.Marshal(model.DomainPurchasePayload{Kind: "domain", Domain: domain, Mailboxes: specs})
	purchaseID, err := model.CreateMailboxPurchase(userID, 0, len(specs), string(payload))
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape(err.Error()))
		return
	}
	if config.WhopDomainAddonID == "" || !whop.IsConfigured() {
		if err := FulfillDomainPurchase(purchaseID); err != nil {
			c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Domain ordered"))
		return
	}
	returnURL := strings.TrimRight(config.BaseURL, "/") + "/mailboxes?domain_purchase_id=" + strconv.FormatInt(purchaseID, 10)
	checkoutURL, err := whop.CreateCheckoutWithPlanID(userID, config.WhopDomainAddonID, returnURL, map[string]string{
		"user_id":            strconv.FormatInt(userID, 10),
		"domain_purchase_id": strconv.FormatInt(purchaseID, 10),
		"purpose":            "domain_addon",
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape(err.Error()))
		return
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "pending_payment", checkoutURL, "", "")
	c.Redirect(http.StatusFound, checkoutURL)
}

func FulfillDomainPurchase(purchaseID int64) error {
	p, err := model.GetMailboxPurchase(purchaseID)
	if err != nil {
		return err
	}
	if p.Status == "fulfilled" || p.Status == "ready" {
		return nil
	}
	payload, err := model.ParseDomainPurchasePayload(p.PayloadJSON)
	if err != nil || payload.Domain == "" {
		_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", "invalid domain purchase payload")
		return fmt.Errorf("invalid domain purchase payload")
	}
	domainID, orderID, err := model.PlaceStarterDomainOrder(p.UserID, payload.Domain, payload.Mailboxes)
	if err != nil {
		_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", err.Error())
		return err
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "provisioning", "", orderID, "")
	_ = model.SyncInboxKitOrder(p.UserID, domainID)
	_ = model.MarkOutreachDomainPaid(domainID)
	_ = model.UpdateMailboxPurchase(purchaseID, "fulfilled", "", orderID, "")
	return nil
}
