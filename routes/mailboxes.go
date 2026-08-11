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
	"emailtracker.com/notify"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
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
	quotaLeft, _ := model.IncludedDomainQuotaRemaining(userID)
	inboxHint := inboxkit.ConfiguredHint()
	if inboxHint != "" {
		inboxHint = config.WithSupportContact(inboxHint)
	}
	c.HTML(http.StatusOK, "onboarding_domain.html", gin.H{
		"title":         "Set up outreach domain",
		"active":        "mailboxes",
		"user":          user,
		"inboxkitOK":    inboxkit.Configured(),
		"inboxkitHint":  inboxHint,
		"includedCount": config.InboxKitIncludedMailboxCount(),
		"mailboxSlots":  mailboxSlotNums(config.InboxKitIncludedMailboxCount()),
		"pendingDomain": pending,
		"includedLeft":  quotaLeft,
		"supportEmail":  config.SupportEmail,
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
	var msg string
	switch {
	case strings.Contains(lower, "already includes one domain"):
		msg = raw
	case strings.Contains(lower, "invalid workspace"):
		msg = "InboxKit rejected the workspace ID. In the InboxKit dashboard open Settings → Workspaces, copy the workspace UUID (not the team name), and set INBOXKIT_WORKSPACE_ID to that value, then restart the app."
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401"):
		msg = "InboxKit API key was rejected. Check INBOXKIT_API_KEY in your environment and restart the app."
	case strings.Contains(lower, "inboxkit_workspace_id not configured"):
		msg = "Set INBOXKIT_WORKSPACE_ID to your InboxKit workspace UUID, then restart the app."
	case strings.Contains(lower, "inboxkit_api_key not configured"):
		msg = "Set INBOXKIT_API_KEY, then restart the app."
	case strings.Contains(lower, "registration contact is not configured"):
		msg = "Domain registration contact is not configured on this server yet."
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
					msg = "InboxKit: " + payload.Message
				}
			}
		}
		if msg == "" {
			msg = raw
		}
	}
	return config.WithSupportContact(msg)
}

func mailboxSlotNums(n int) []int {
	if n < 1 {
		n = 1
	}
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
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
	quotaLeft, _ := model.IncludedDomainQuotaRemaining(userID)
	c.HTML(http.StatusOK, "onboarding_domain.html", gin.H{
		"title":         "Set up outreach domain",
		"active":        "mailboxes",
		"user":          user,
		"inboxkitOK":    true,
		"includedCount": config.InboxKitIncludedMailboxCount(),
		"mailboxSlots":  mailboxSlotNums(config.InboxKitIncludedMailboxCount()),
		"includedLeft":  quotaLeft,
		"supportEmail":  config.SupportEmail,
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
	domainID, orderID, err := model.PlaceStarterDomainOrder(userID, domain, specs, true)
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
	var domainMailboxes []model.OutreachMailbox
	for _, m := range mailboxes {
		if m.DomainID == domainID {
			domainMailboxes = append(domainMailboxes, m)
		}
	}
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
	errMsg := c.Query("error")
	if errMsg == "" && d.LastError != "" {
		errMsg = d.LastError
	}
	if errMsg != "" {
		errMsg = humanizeInboxKitError(errMsg)
	}
	c.HTML(http.StatusOK, "onboarding_domain_status.html", gin.H{
		"title":         "Setting up domain",
		"active":        "mailboxes",
		"domain":        d,
		"mailboxes":     domainMailboxes,
		"nameservers":   nameservers,
		"nsPropagated":  nsPropagated,
		"needsNS":       needsNS && !model.IsManualPendingDomain(d),
		"manualPending": model.IsManualPendingDomain(d),
		"supportEmail":  config.SupportEmail,
		"error":         errMsg,
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
	// Fast local prune (no InboxKit). Heavy ensure/sync runs in the background.
	_ = model.PruneAdminOutreachExtras(userID)
	model.ScheduleAdminOutreachEnsure(userID)
	model.SchedulePendingOutreachSync(userID)

	domains, _ := model.ListOutreachDomains(userID)
	mailboxes, _ := model.ListOutreachMailboxes(userID)
	smtpByID, _ := model.MapSMTPAccountsByID(userID)
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

	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	statusFilter := strings.ToLower(strings.TrimSpace(c.Query("status")))
	domainByID := map[int64]model.OutreachDomain{}
	activeDomains := 0
	for _, d := range domains {
		domainByID[d.ID] = d
		if d.Status == "ready" {
			activeDomains++
		}
	}

	type mailboxRow struct {
		ID              int64
		Email           string
		DomainName      string
		Status          string
		StatusLabel     string
		IsDefault       bool
		IsAdmin         bool
		Role            string
		FromName        string
		Initial         string
		Platform        string
		WarmupLabel     string
		WarmupReady     bool
		CampaignsLabel  string
		RenewalLabel    string
		InsightsHint    string
		Source          string
		CanEditCreds    bool
		IsInboxKit      bool
		ForwardingEmail string
		LastError       string
		SMTPHost        string
		SMTPPort        string
		IMAPHost        string
		IMAPPort        string
		SMTPUser        string
		WarmupEnabled   bool
		DailyLimit      int
		SendsToday      int
		CreatedAt       string
		UpdatedAt       string
	}
	var rows []mailboxRow
	activeMailboxes := 0
	renewalLabel := "—"
	if user.SubscriptionEndsAt != nil {
		renewalLabel = user.SubscriptionEndsAt.Format("1/2/2006")
	}

	for _, m := range mailboxes {
		if q != "" {
			hay := strings.ToLower(m.Email + " " + m.FirstName + " " + m.LastName)
			if d, ok := domainByID[m.DomainID]; ok {
				hay += " " + strings.ToLower(d.Domain)
			}
			if !strings.Contains(hay, q) {
				continue
			}
		}
		st := strings.ToLower(m.Status)
		if statusFilter != "" && statusFilter != "all" {
			if statusFilter == "active" && st != "ready" && st != "active" {
				continue
			}
			if statusFilter == "scheduled_cancel" && st != "scheduled_cancel" && st != "scheduled_for_cancellation" {
				continue
			}
			if statusFilter != "active" && statusFilter != "scheduled_cancel" && st != statusFilter {
				continue
			}
		}

		fromName := strings.TrimSpace(m.FirstName + " " + m.LastName)
		initial := "M"
		if fromName != "" {
			initial = strings.ToUpper(string([]rune(fromName)[0]))
		} else if m.Email != "" {
			initial = strings.ToUpper(string(m.Email[0]))
		}
		domainName := ""
		if d, ok := domainByID[m.DomainID]; ok {
			domainName = d.Domain
		} else if i := strings.Index(m.Email, "@"); i >= 0 {
			domainName = m.Email[i+1:]
		}
		statusLabel := m.Status
		switch st {
		case "ready", "active":
			statusLabel = "Active"
			activeMailboxes++
		case "scheduled_cancel", "scheduled_for_cancellation":
			statusLabel = "Scheduled For Cancellation"
		case "provisioning":
			statusLabel = "Provisioning"
		case "error":
			statusLabel = "Error"
		}
		role := m.Role
		if m.IsAdmin && role == "" {
			role = "Admin"
		}
		row := mailboxRow{
			ID: m.ID, Email: m.Email, DomainName: domainName, Status: m.Status, StatusLabel: statusLabel,
			IsDefault: m.IsDefault, IsAdmin: m.IsAdmin || strings.EqualFold(role, "admin"), Role: role,
			FromName: fromName, Initial: initial, Platform: m.Platform,
			ForwardingEmail: m.ForwardingEmail, LastError: m.LastError,
			CreatedAt: m.CreatedAt.Format("1/2/2006"), UpdatedAt: m.UpdatedAt.Format("1/2/2006"),
			CampaignsLabel: "—", RenewalLabel: renewalLabel, WarmupLabel: "—",
		}
		if m.SMTPAccountID > 0 {
			if acc, ok := smtpByID[m.SMTPAccountID]; ok {
				if strings.TrimSpace(acc.FromName) != "" {
					row.FromName = acc.FromName
					row.Initial = strings.ToUpper(string([]rune(acc.FromName)[0]))
				}
				row.Source = acc.MailboxSource
				row.CanEditCreds = acc.MailboxSource == model.MailboxSourceManual || acc.MailboxSource == model.MailboxSourceInboxKit
				row.IsInboxKit = acc.MailboxSource == model.MailboxSourceInboxKit
				row.SMTPHost = acc.SMTPHost
				row.SMTPPort = acc.SMTPPort
				row.IMAPHost = acc.IMAPHost
				row.IMAPPort = acc.IMAPPort
				row.SMTPUser = acc.SMTPUser
				row.WarmupEnabled = acc.WarmupEnabled
				row.DailyLimit = acc.DailyLimit
				row.SendsToday = acc.SendsToday
				schedule := outbound.EffectiveDailyCap(acc)
				cap, hint := outbound.ApplyInsightsToCap(schedule, acc, m.AnalyticsJSON)
				if acc.WarmupEnabled {
					row.WarmupLabel = fmt.Sprintf("%d/%d", acc.SendsToday, cap)
					row.WarmupReady = true
				} else {
					row.WarmupLabel = "Add"
					row.WarmupReady = false
				}
				if hint.Adjusted {
					row.InsightsHint = hint.Reason
				}
				row.CampaignsLabel = "—"
			}
		} else if st == "ready" || st == "active" {
			row.WarmupLabel = "Add"
		}
		if row.Source == "" {
			if strings.EqualFold(m.Platform, "MANUAL") {
				row.Source = model.MailboxSourceManual
				row.CanEditCreds = m.SMTPAccountID > 0
			} else if m.InboxkitMailboxID != "" || strings.EqualFold(m.Platform, "GOOGLE") {
				row.Source = model.MailboxSourceInboxKit
				row.IsInboxKit = true
			}
		}
		if strings.EqualFold(row.Platform, "") {
			row.Platform = "GOOGLE"
		}
		rows = append(rows, row)
	}

	manageID, _ := strconv.ParseInt(c.Query("manage"), 10, 64)
	manageTab := c.Query("tab")
	if manageTab == "" {
		manageTab = "overview"
	}
	var manageRow *mailboxRow
	if manageID > 0 {
		for i := range rows {
			if rows[i].ID == manageID {
				manageRow = &rows[i]
				break
			}
		}
		if manageRow == nil {
			// Still allow manage for filtered-out rows.
			if m, err := model.GetOutreachMailbox(manageID, userID); err == nil {
				fromName := strings.TrimSpace(m.FirstName + " " + m.LastName)
				domainName := ""
				if d, ok := domainByID[m.DomainID]; ok {
					domainName = d.Domain
				}
				r := mailboxRow{
					ID: m.ID, Email: m.Email, DomainName: domainName, Status: m.Status,
					FromName: fromName, IsDefault: m.IsDefault, IsAdmin: m.IsAdmin, Role: m.Role,
					Platform: m.Platform, ForwardingEmail: m.ForwardingEmail, LastError: m.LastError,
					CreatedAt: m.CreatedAt.Format("1/2/2006"), UpdatedAt: m.UpdatedAt.Format("1/2/2006"),
					IsInboxKit: m.InboxkitMailboxID != "",
				}
				if m.SMTPAccountID > 0 {
					if acc, ok := smtpByID[m.SMTPAccountID]; ok {
						r.FromName = firstNonEmptyStr(acc.FromName, r.FromName)
						r.SMTPHost, r.SMTPPort = acc.SMTPHost, acc.SMTPPort
						r.IMAPHost, r.IMAPPort = acc.IMAPHost, acc.IMAPPort
						r.SMTPUser = acc.SMTPUser
						r.Source = acc.MailboxSource
						r.CanEditCreds = acc.MailboxSource != model.MailboxSourceShared
						r.WarmupEnabled = acc.WarmupEnabled
						r.DailyLimit = acc.DailyLimit
						r.SendsToday = acc.SendsToday
						r.IsInboxKit = acc.MailboxSource == model.MailboxSourceInboxKit
					}
				}
				manageRow = &r
			}
		}
	}

	showAttach := c.Query("attach") == "1"

	var readySMTP []model.SMTPAccount
	for _, acc := range smtpByID {
		if acc.IsSendReady() {
			readySMTP = append(readySMTP, acc)
		}
	}
	combinedWarmup := outbound.ComputeCombinedWarmupProgress(readySMTP)

	c.HTML(http.StatusOK, "mailboxes.html", gin.H{
		"title":           "Mailboxes",
		"active":          "mailboxes",
		"user":            user,
		"domains":         domains,
		"mailboxes":       mailboxes,
		"mailboxRows":     rows,
		"statsActiveDomains": activeDomains,
		"statsTotal":      len(mailboxes),
		"statsActive":     activeMailboxes,
		"isPro":           isPro,
		"isAdmin":         model.UserIsAdmin(user),
		"sharedReady":     sharedReady,
		"sharedEmail":     sharedEmail,
		"inboxkitOK":      inboxkit.Configured(),
		"whopAddon":       config.WhopMailboxAddonID != "" && whop.IsConfigured(),
		"success":         c.Query("success"),
		"error":           humanizeInboxKitError(c.Query("error")),
		"supportEmail":    config.SupportEmail,
		"includedCount":   config.InboxKitIncludedMailboxCount(),
		"q":               c.Query("q"),
		"statusFilter":    statusFilter,
		"manageRow":       manageRow,
		"manageTab":       manageTab,
		"showAttach":      showAttach,
		"combinedWarmup":  combinedWarmup,
		"playbook":        playbookMailboxes(),
	})
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
	if username == "" {
		username = email
	}
	if smtpHost == "" {
		smtpHost = "smtp.gmail.com"
	}
	if smtpPort == "" {
		smtpPort = "587"
	}
	oid, err := model.AttachMailboxSmart(userID, email, fromName, smtpHost, smtpPort, imapHost, imapPort, username, password, isDefault)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(humanizeInboxKitError(err.Error())))
		return
	}
	if m, mErr := model.GetOutreachMailbox(oid, userID); mErr == nil && m.SMTPAccountID > 0 {
		if _, probeErr := runUserSMTPCheck(userID, m.SMTPAccountID); probeErr != nil {
			c.Redirect(http.StatusFound, mailboxManageURL(oid, "credentials", "Connected but SMTP failed: "+probeErr.Error(), ""))
			return
		}
	}
	c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailbox connected"))
}

func MailboxesUpdateCredentials(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	smtpHost := strings.TrimSpace(c.PostForm("smtp_host"))
	smtpPort := strings.TrimSpace(c.PostForm("smtp_port"))
	imapHost := strings.TrimSpace(c.PostForm("imap_host"))
	imapPort := strings.TrimSpace(c.PostForm("imap_port"))
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	m, err := model.GetOutreachMailbox(id, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape("Mailbox not found"))
		return
	}
	if m.SMTPAccountID <= 0 {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape("No SMTP credentials on this mailbox"))
		return
	}
	acc, err := model.GetSMTPAccount(m.SMTPAccountID)
	if err != nil || acc.UserID != userID {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape("SMTP account not found"))
		return
	}
	probeHost := smtpHost
	if probeHost == "" {
		probeHost = acc.SMTPHost
	}
	probePort := smtpPort
	if probePort == "" {
		probePort = acc.SMTPPort
	}
	probeUser := username
	if probeUser == "" {
		probeUser = acc.SMTPUser
	}
	probePass := password
	if strings.TrimSpace(probePass) == "" {
		probePass, err = model.DecryptSMTPPassword(acc)
		if err != nil {
			c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape("Could not read existing password — enter a new one"))
			return
		}
	}
	from := acc.SenderEmail()
	if from == "" {
		from = m.Email
	}
	if err := util.ProbeSMTPPlain(probeHost, probePort, probeUser, probePass, from); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", formatSMTPProbeError(probeHost, probePort, from, err).Error(), ""))
		return
	}
	if err := model.UpdateMailboxCredentials(userID, id, smtpHost, smtpPort, imapHost, imapPort, username, password); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", err.Error(), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "Credentials updated"))
}

func mailboxManageURL(id int64, tab, errMsg, success string) string {
	u := "/mailboxes?manage=" + strconv.FormatInt(id, 10) + "&tab=" + url.QueryEscape(tab)
	if errMsg != "" {
		u += "&error=" + url.QueryEscape(errMsg)
	}
	if success != "" {
		u += "&success=" + url.QueryEscape(success)
	}
	return u
}

func MailboxesDelete(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.DeleteOutreachMailbox(userID, id); err != nil {
		c.Redirect(http.StatusFound, "/mailboxes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape("Mailbox deleted"))
}

func MailboxesSetDefault(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.SetDefaultOutreachMailbox(userID, id); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", err.Error(), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", "", "Default mailbox updated"))
}

func MailboxesUpdateFromName(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	fromName := strings.TrimSpace(c.PostForm("from_name"))
	if err := model.UpdateMailboxFromName(userID, id, fromName); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", err.Error(), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", "", "From name updated"))
}

func MailboxesRefreshCredentials(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.RefreshMailboxFromInboxKit(userID, id); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", humanizeInboxKitError(err.Error()), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "Credentials refreshed from InboxKit"))
}

func MailboxesRevealPassword(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	pass, err := model.DecryptMailboxPassword(userID, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"password": pass})
}

func MailboxesUpdateWarmup(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	enabled := c.PostForm("warmup_enabled") == "1" || c.PostForm("warmup_enabled") == "on"
	daily, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("daily_limit")))
	if err := model.UpdateMailboxWarmupSettings(userID, id, enabled, daily); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", err.Error(), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", "", "Warmup settings saved"))
}

func MailboxesCancel(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.CancelInboxKitMailbox(userID, id); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "settings", humanizeInboxKitError(err.Error()), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "overview", "", "Mailbox scheduled for cancellation at InboxKit"))
}

func MailboxesForwarding(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	action := strings.TrimSpace(c.PostForm("action"))
	fwd := strings.TrimSpace(c.PostForm("forwarding_email"))
	remove := action == "remove"
	if err := model.SetMailboxForwarding(userID, id, fwd, remove); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "forwarding", humanizeInboxKitError(err.Error()), ""))
		return
	}
	msg := "Forwarding updated — InboxKit may take a few minutes to apply"
	if remove {
		msg = "Forwarding removed"
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "forwarding", "", msg))
}

func MailboxesBuyPage(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Buying mailboxes requires Pro"))
		return
	}
	if !inboxkit.Configured() {
		c.HTML(http.StatusOK, "mailboxes_buy.html", gin.H{
			"title":  "Buy mailboxes",
			"active": "mailboxes",
			"error":  inboxkit.ConfiguredHint(),
		})
		return
	}
	domains, _ := model.ListOutreachDomains(userID)
	var ready []model.OutreachDomain
	var pending []model.OutreachDomain
	for _, d := range domains {
		if d.Status == "ready" {
			ready = append(ready, d)
		} else if d.Status != "error" {
			pending = append(pending, d)
		}
	}
	if len(ready) == 0 {
		msg := "Set up a domain first, then buy more mailboxes."
		if len(pending) > 0 {
			msg = fmt.Sprintf("Finish setting up %s before buying more seats.", pending[0].Domain)
			c.Redirect(http.StatusFound, "/onboarding/domain/status?domain_id="+strconv.FormatInt(pending[0].ID, 10)+"&error="+url.QueryEscape(msg))
			return
		}
		c.Redirect(http.StatusFound, "/onboarding/domain?new=1&error="+url.QueryEscape(msg))
		return
	}
	c.HTML(http.StatusOK, "mailboxes_buy.html", gin.H{
		"title":   "Buy mailboxes",
		"active":  "mailboxes",
		"domains": ready,
		"error":   humanizeInboxKitError(c.Query("error")),
		"whopOK":  config.WhopMailboxAddonID != "" && whop.IsConfigured(),
	})
}

func MailboxesBuyCheckout(c *gin.Context) {
	userID := mustUserID(c)
	if !model.UserIsPro(userID) {
		c.Redirect(http.StatusFound, "/settings/billing?error="+url.QueryEscape("Buying mailboxes requires Pro"))
		return
	}
	if !inboxkit.Configured() {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape(inboxkit.ConfiguredHint()))
		return
	}
	domainID, _ := strconv.ParseInt(c.PostForm("domain_id"), 10, 64)
	qty, _ := strconv.Atoi(c.PostForm("quantity"))
	if qty < 1 {
		qty = 1
	}
	if qty > 5 {
		qty = 5
	}
	d, err := model.GetOutreachDomain(domainID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape("Pick a domain"))
		return
	}
	if d.Status != "ready" {
		c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape("That domain is not ready yet — finish setup first"))
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
		if pErr := FulfillMailboxPurchase(purchaseID); pErr != nil {
			log.Printf("buy mailboxes user=%d domain=%s: %v", userID, d.Domain, pErr)
			c.Redirect(http.StatusFound, "/mailboxes/buy?error="+url.QueryEscape(humanizeInboxKitError(pErr.Error())))
			return
		}
		msg := "Mailbox ordered — it will appear as Active once credentials are ready"
		if config.ManualInboxKitFulfillment {
			msg = "Mailbox request received — setup usually takes about 2 hours. We'll email you when it's ready."
		}
		c.Redirect(http.StatusFound, "/mailboxes?success="+url.QueryEscape(msg))
		return
	}

	returnURL := strings.TrimRight(config.BaseURL, "/") + "/mailboxes?purchase_id=" + strconv.FormatInt(purchaseID, 10)
	checkoutURL, err := whop.CreateCheckoutWithPlanID(userID, config.WhopMailboxAddonID, returnURL, map[string]string{
		"user_id":             strconv.FormatInt(userID, 10),
		"mailbox_purchase_id": strconv.FormatInt(purchaseID, 10),
		"purpose":             "mailbox_addon",
	})
	if err != nil {
		log.Printf("whop mailbox checkout: %v", err)
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

	var domainName string
	var emails []string
	if d, dErr := model.GetOutreachDomain(p.DomainID, p.UserID); dErr == nil {
		domainName = d.Domain
		for _, s := range specs {
			local := strings.TrimSpace(s.LocalPart)
			if local == "" {
				local = strings.TrimSpace(s.FirstName + "." + s.LastName)
			}
			emails = append(emails, strings.ToLower(local)+"@"+domainName)
		}
	}

	// First payment webhook / checkout: queue. Admin re-entry with pending_manual: place now.
	if config.ManualInboxKitFulfillment && strings.ToLower(p.Status) != "pending_manual" && p.Status != "needs_support" {
		return model.QueueMailboxPurchase(purchaseID, "extra mailboxes", domainName, emails)
	}

	prevManual := config.ManualInboxKitFulfillment
	config.ManualInboxKitFulfillment = false
	defer func() { config.ManualInboxKitFulfillment = prevManual }()

	orderID, err := model.PlaceExtraMailboxesOrder(p.UserID, p.DomainID, specs)
	if err != nil {
		_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", err.Error())
		return err
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "provisioning", "", orderID, "")
	_ = model.SyncInboxKitOrder(p.UserID, p.DomainID)
	_ = model.UpdateMailboxPurchase(purchaseID, "fulfilled", "", orderID, "")
	email, label := userNotifyMeta(p.UserID)
	notify.NotifyProvisionReady(email, label, domainName)
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
		"mailboxSlots":  mailboxSlotNums(config.InboxKitIncludedMailboxCount()),
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
		"mailboxSlots":  mailboxSlotNums(config.InboxKitIncludedMailboxCount()),
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

	// Already queued: fulfill the domain row via InboxKit.
	if d, dErr := model.GetOutreachDomainByName(p.UserID, payload.Domain); dErr == nil && model.IsManualPendingDomain(d) {
		if err := model.FulfillQueuedDomainOrder(d.ID); err != nil {
			_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", err.Error())
			return err
		}
		_ = model.UpdateMailboxPurchase(purchaseID, "fulfilled", "", "", "")
		_ = model.MarkOutreachDomainPaid(d.ID)
		return nil
	}

	domainID, orderID, err := model.PlaceStarterDomainOrder(p.UserID, payload.Domain, payload.Mailboxes, false)
	if err != nil {
		_ = model.UpdateMailboxPurchase(purchaseID, "needs_support", "", "", err.Error())
		return err
	}
	if strings.HasPrefix(orderID, "manual:") {
		_ = model.UpdateMailboxPurchase(purchaseID, "pending_manual", "", orderID, "")
		return nil
	}
	_ = model.UpdateMailboxPurchase(purchaseID, "provisioning", "", orderID, "")
	_ = model.SyncInboxKitOrder(p.UserID, domainID)
	_ = model.MarkOutreachDomainPaid(domainID)
	_ = model.UpdateMailboxPurchase(purchaseID, "fulfilled", "", orderID, "")
	return nil
}
