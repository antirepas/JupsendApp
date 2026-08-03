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
		ensureAdminPro(userID)

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

func ensureAdminPro(userID int64) {
	if u, err := model.GetUserByID(userID); err == nil && model.UserIsAdmin(u) {
		_ = model.EnsureAdminProAccess(userID)
	}
}

func OnboardingDomainPage(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	// Stay on this page when showing flash messages or forcing a new setup.
	// Otherwise users with any ready mailbox (including shared/manual) never see errors.
	hasFlash := c.Query("error") != "" || c.Query("success") != "" || c.Query("q") != ""
	if model.UserHasReadyMailbox(userID) && c.Query("new") != "1" && !hasFlash {
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
		"title":         "Set up outreach domain",
		"active":        "mailboxes",
		"user":          user,
		"inboxkitOK":    inboxkit.Configured(),
		"inboxkitHint":  inboxkit.ConfiguredHint(),
		"includedCount": config.InboxKitIncludedMailboxCount(),
		"pendingDomain": pending,
		"error":         humanizeInboxKitError(c.Query("error")),
		"success":       c.Query("success"),
		"query":         c.Query("q"),
		"connectDomain": c.Query("domain"),
	})
}

func onboardingDomainErrorURL(msg, q string) string {
	u := "/onboarding/domain?new=1&error=" + url.QueryEscape(humanizeInboxKitError(msg))
	if q != "" {
		u += "&q=" + url.QueryEscape(q)
	}
	return u
}

func humanizeInboxKitError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "invalid workspace"):
		return "InboxKit rejected the workspace ID. In the InboxKit dashboard open Settings → Workspaces, copy the workspace UUID (not the team name), and set INBOXKIT_WORKSPACE_ID to that value, then restart the app."
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401"):
		return "InboxKit API key was rejected. Check INBOXKIT_API_KEY in your environment and restart the app."
	case strings.Contains(lower, "inboxkit_workspace_id not configured"):
		return "Set INBOXKIT_WORKSPACE_ID to your InboxKit workspace UUID, then restart the app."
	case strings.Contains(lower, "inboxkit_api_key not configured"):
		return "Set INBOXKIT_API_KEY, then restart the app."
	default:
		// Prefer the API message body when present.
		if i := strings.Index(raw, "—"); i >= 0 {
			tail := strings.TrimSpace(raw[i+len("—"):])
			if strings.HasPrefix(tail, "{") {
				var payload struct {
					Message string `json:"message"`
					Code    int    `json:"code"`
				}
				if json.Unmarshal([]byte(tail), &payload) == nil && payload.Message != "" {
					if strings.EqualFold(payload.Message, "Invalid workspace") {
						return humanizeInboxKitError("Invalid workspace")
					}
					return "InboxKit: " + payload.Message
				}
			}
		}
		return raw
	}
}

func OnboardingDomainSearch(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	q := strings.TrimSpace(c.PostForm("q"))
	if q == "" {
		q = strings.TrimSpace(c.Query("q"))
	}
	if q == "" {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL("Enter a company or keyword", ""))
		return
	}
	if !inboxkit.Configured() {
		hint := inboxkit.ConfiguredHint()
		if hint == "" {
			hint = "InboxKit is not configured on this server"
		}
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(hint, q))
		return
	}
	client := inboxkit.NewClient()
	results, err := client.SearchDomains(q)
	if err != nil {
		log.Printf("inboxkit domain search %q: %v", q, err)
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(err.Error(), q))
		return
	}
	user, _ := model.GetUserByID(userID)
	c.HTML(http.StatusOK, "onboarding_domain.html", gin.H{
		"title":         "Set up outreach domain",
		"active":        "mailboxes",
		"user":          user,
		"inboxkitOK":    true,
		"includedCount": config.InboxKitIncludedMailboxCount(),
		"query":         q,
		"results":       results,
		"searched":      true,
	})
}

func collectStarterMailboxSpecs(c *gin.Context) ([]model.StarterMailboxSpec, error) {
	n := config.InboxKitIncludedMailboxCount()
	var specs []model.StarterMailboxSpec
	for i := 1; i <= n; i++ {
		fn := strings.TrimSpace(c.PostForm(fmt.Sprintf("first_name_%d", i)))
		ln := strings.TrimSpace(c.PostForm(fmt.Sprintf("last_name_%d", i)))
		local := strings.TrimSpace(c.PostForm(fmt.Sprintf("local_%d", i)))
		if fn == "" && ln == "" && local == "" {
			continue
		}
		if fn == "" || ln == "" || local == "" {
			return nil, fmt.Errorf("mailbox %d needs first name, last name, and email local part (recipients see the name as From)", i)
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("add at least one mailbox with first name, last name, and local part")
	}
	return specs, nil
}

func OnboardingDomainPurchase(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	domain := strings.ToLower(strings.TrimSpace(c.PostForm("domain")))
	if domain == "" || !strings.Contains(domain, ".") {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL("Pick a valid domain", ""))
		return
	}
	specs, err := collectStarterMailboxSpecs(c)
	if err != nil {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(err.Error(), domain))
		return
	}
	domainID, orderID, err := model.PlaceStarterDomainOrder(userID, domain, specs)
	if err != nil {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(err.Error(), domain))
		return
	}
	_ = orderID
	c.Redirect(http.StatusFound, "/onboarding/domain/status?domain_id="+strconv.FormatInt(domainID, 10))
}

func OnboardingDomainConnect(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom domains require Pro"))
		return
	}
	domain := strings.ToLower(strings.TrimSpace(c.PostForm("domain")))
	if domain == "" || !strings.Contains(domain, ".") {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL("Enter a valid domain you already own", ""))
		return
	}
	specs, err := collectStarterMailboxSpecs(c)
	if err != nil {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(err.Error(), "")+"&domain="+url.QueryEscape(domain))
		return
	}
	domainID, _, _, err := model.PlaceConnectExistingDomainOrder(userID, domain, specs)
	if err != nil {
		log.Printf("connect domain %s: %v", domain, err)
		c.Redirect(http.StatusFound, onboardingDomainErrorURL(err.Error(), "")+"&domain="+url.QueryEscape(domain))
		return
	}
	c.Redirect(http.StatusFound, "/onboarding/domain/status?domain_id="+strconv.FormatInt(domainID, 10))
}

func OnboardingDomainStatus(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	domainID, _ := strconv.ParseInt(c.Query("domain_id"), 10, 64)
	d, err := model.GetOutreachDomain(domainID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, onboardingDomainErrorURL("Domain setup not found", ""))
		return
	}
	_ = model.SyncInboxKitOrder(userID, domainID)
	d, _ = model.GetOutreachDomain(domainID, userID)
	if d.Status == "ready" && model.UserHasReadyMailbox(userID) {
		c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailboxes ready — you can start sending"))
		return
	}
	mailboxes, _ := model.ListOutreachMailboxes(userID)
	nameservers := d.Nameservers()
	nsPropagated := false
	needsNS := inboxkit.IsConnectOrderID(d.InboxkitOrderID) || d.Status == "connecting"
	if inboxkit.Configured() && d.Domain != "" && needsNS {
		client := inboxkit.NewClient()
		if check, checkErr := client.CheckNameservers(d.Domain); checkErr == nil {
			nsPropagated = check.Propagated || check.Ready
		} else {
			log.Printf("domain status check NS %s: %v", d.Domain, checkErr)
		}
		// Only create/fetch NS if we never stored them (legacy rows).
		if len(nameservers) == 0 {
			if ns, nsErr := client.ConnectDomainNameservers(d.Domain); nsErr == nil {
				nameservers = ns.Nameservers
				_ = model.SetOutreachDomainNameservers(d.ID, nameservers)
			} else {
				log.Printf("domain status nameservers %s: %v", d.Domain, nsErr)
			}
		}
	}
	c.HTML(http.StatusOK, "onboarding_domain_status.html", gin.H{
		"title":        "Setting up domain",
		"active":       "mailboxes",
		"domain":       d,
		"mailboxes":    mailboxes,
		"nameservers":  nameservers,
		"nsPropagated": nsPropagated,
		"needsNS":      needsNS,
		"error":        c.Query("error"),
	})
}

func MailboxesPage(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
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
		FromName     string
		WarmupLabel  string
		InsightsHint string
	}
	var rows []mailboxRow
	for _, m := range mailboxes {
		fromName := strings.TrimSpace(m.FirstName + " " + m.LastName)
		row := mailboxRow{ID: m.ID, Email: m.Email, Status: m.Status, IsDefault: m.IsDefault, FromName: fromName}
		if m.SMTPAccountID > 0 {
			if acc, err := model.GetSMTPAccount(m.SMTPAccountID); err == nil {
				if strings.TrimSpace(acc.FromName) != "" {
					row.FromName = acc.FromName
				}
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
		"isAdmin":       model.UserIsAdmin(user),
		"sharedReady":   sharedReady,
		"sharedEmail":   sharedEmail,
		"inboxkitOK":    inboxkit.Configured(),
		"whopAddon":     config.WhopMailboxAddonID != "" && whop.IsConfigured(),
		"success":       c.Query("success"),
		"error":         c.Query("error"),
		"includedCount": config.InboxKitIncludedMailboxCount(),
	})
}

func MailboxesAttachManual(c *gin.Context) {
	userID := mustUserID(c)
	ensureAdminPro(userID)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Custom mailboxes require Pro"))
		return
	}
	email := strings.TrimSpace(c.PostForm("email"))
	fromName := strings.TrimSpace(c.PostForm("from_name"))
	if fromName == "" {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape("From name is required — recipients see this on every email"))
		return
	}
	smtpHost := strings.TrimSpace(c.PostForm("smtp_host"))
	smtpPort := strings.TrimSpace(c.PostForm("smtp_port"))
	imapHost := strings.TrimSpace(c.PostForm("imap_host"))
	imapPort := strings.TrimSpace(c.PostForm("imap_port"))
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	isDefault := c.PostForm("is_default") == "1" || c.PostForm("is_default") == "on"
	_, err := model.AttachManualSendingMailbox(userID, email, fromName, smtpHost, smtpPort, imapHost, imapPort, username, password, isDefault)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailbox connected — sending and reply tracking use these SMTP/IMAP credentials"))
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

func MailboxesUpdateFromName(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	fromName := strings.TrimSpace(c.PostForm("from_name"))
	if err := model.UpdateMailboxFromName(userID, id, fromName); err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("From name updated"))
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
		if fn == "" || ln == "" || local == "" {
			c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape(fmt.Sprintf("Mailbox %d needs first name, last name, and local part (recipients see this as From)", i)))
			return
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
		if fn == "" && ln == "" && local == "" {
			continue
		}
		if fn == "" || ln == "" || local == "" {
			c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape(fmt.Sprintf("Mailbox %d needs first name, last name, and local part", i)))
			return
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	if len(specs) == 0 {
		c.Redirect(http.StatusFound, "/mailboxes/domains/buy?error="+url.QueryEscape("Add at least one mailbox with first name, last name, and local part"))
		return
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
