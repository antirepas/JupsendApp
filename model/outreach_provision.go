package model

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/inboxkit"
)

type StarterMailboxSpec struct {
	FirstName string
	LastName  string
	LocalPart string
}

// Injectable InboxKit calls (overridden in tests).
var (
	createInboxKitOrder = func(req inboxkit.CreateOrderRequest) (inboxkit.CreateOrderResponse, error) {
		return inboxkit.NewClient().CreateOrder(req)
	}
	buyInboxKitMailboxes = func(req inboxkit.BuyMailboxesRequest) (inboxkit.BuyMailboxesResponse, error) {
		return inboxkit.NewClient().BuyMailboxes(req)
	}
	connectInboxKitNameservers = func(domain string) (inboxkit.NameserverResult, error) {
		return inboxkit.NewClient().ConnectDomainNameservers(domain)
	}
	checkInboxKitNameservers = func(domain string) (inboxkit.NameserverResult, error) {
		return inboxkit.NewClient().CheckNameservers(domain)
	}
)

const pendingBuyIDPrefix = "pending-buy:"
const pendingOrderIDPrefix = "pending:"

func isPendingBuyMailboxID(id string) bool {
	return strings.HasPrefix(id, pendingBuyIDPrefix)
}

func isPendingOrderID(id string) bool {
	return strings.HasPrefix(id, pendingOrderIDPrefix)
}

func assertIncludedDomainQuota(userID int64) error {
	left, err := IncludedDomainQuotaRemaining(userID)
	if err != nil {
		return err
	}
	if left <= 0 {
		return fmt.Errorf("your plan already includes one domain — buy an extra domain from Mailboxes, or continue setup for your existing domain")
	}
	return nil
}

func buildOrderMailboxes(domain string, specs []StarterMailboxSpec, platform string) []inboxkit.OrderMailbox {
	var mboxes []inboxkit.OrderMailbox
	for _, s := range specs {
		local := sanitizeLocalPart(s.LocalPart)
		if local == "" {
			local = sanitizeLocalPart(s.FirstName + "." + s.LastName)
		}
		if local == "" {
			local = "hello"
		}
		mboxes = append(mboxes, inboxkit.OrderMailbox{
			FirstName: s.FirstName,
			LastName:  s.LastName,
			Email:     local + "@" + domain,
			Username:  local,
			Platform:  platform,
		})
	}
	return mboxes
}

func ensureStarterMailboxRows(userID, domainID int64, mboxes []inboxkit.OrderMailbox, platform string, included bool) error {
	var firstErr error
	for i, mb := range mboxes {
		_, err := UpsertOutreachMailbox(OutreachMailbox{
			UserID:    userID,
			DomainID:  domainID,
			Email:     mb.Email,
			FirstName: mb.FirstName,
			LastName:  mb.LastName,
			Platform:  platform,
			Status:    "provisioning",
			IsDefault: i == 0,
			Included:  included,
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func registrantOrError() (map[string]any, error) {
	reg := inboxkit.DefaultRegistrant()
	if len(reg) == 0 {
		return nil, fmt.Errorf("domain registration contact is not configured on this server")
	}
	return reg, nil
}

// PlaceStarterDomainOrder buys a domain + mailboxes via InboxKit.
// When included is true, enforces the Pro included-domain quota.
// When config.ManualInboxKitFulfillment is on, queues for ~2h manual fulfill instead.
func PlaceStarterDomainOrder(userID int64, domain string, specs []StarterMailboxSpec, included bool) (domainID int64, orderID string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || !strings.Contains(domain, ".") {
		return 0, "", fmt.Errorf("pick a valid domain")
	}
	n := config.InboxKitIncludedMailboxCount()
	if len(specs) == 0 {
		return 0, "", fmt.Errorf("at least one mailbox is required")
	}
	if len(specs) > n {
		specs = specs[:n]
	}

	if config.ManualInboxKitFulfillment {
		return queueStarterDomainOrder(userID, domain, specs, included)
	}
	if !inboxkit.Configured() {
		return 0, "", fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}

	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}
	mboxes := buildOrderMailboxes(domain, specs, platform)

	// Idempotent resume: same domain already ordered / in progress.
	if existing, gErr := GetOutreachDomainByName(userID, domain); gErr == nil {
		st := strings.ToLower(existing.Status)
		oid := strings.TrimSpace(existing.InboxkitOrderID)
		if st == "pending_manual" || isManualOrderID(oid) {
			if config.ManualInboxKitFulfillment {
				return queueStarterDomainOrder(userID, domain, specs, included)
			}
			// Fulfill path: continue and place a real InboxKit order.
		} else if st != "error" && oid != "" && !isPendingOrderID(oid) {
			_ = ensureStarterMailboxRows(userID, existing.ID, mboxes, platform, existing.Included)
			return existing.ID, oid, nil
		}
	}

	if included {
		if _, gErr := GetOutreachDomainByName(userID, domain); gErr != nil {
			if qErr := assertIncludedDomainQuota(userID); qErr != nil {
				return 0, "", qErr
			}
		}
	}

	reg, regErr := registrantOrError()
	if regErr != nil {
		return 0, "", regErr
	}

	// Reserve a local row before calling InboxKit so double-submit races share one claim.
	pendingID := pendingOrderIDPrefix + fmt.Sprintf("%d-%d", userID, time.Now().UnixNano())
	domainID, err = CreateOutreachDomain(userID, domain, pendingID, redirect, included)
	if err != nil {
		return 0, "", fmt.Errorf("could not start domain setup: %w", err)
	}
	if included {
		if nActive, cErr := CountActiveIncludedDomains(userID); cErr == nil {
			spec, _ := PlanSpecForTier(PlanTierPro)
			if nActive > spec.IncludedDomains {
				var older int64
				_ = db.QueryRow(`
					SELECT id FROM outreach_domains
					WHERE user_id=? AND included=TRUE
					  AND lower(status) NOT IN ('error','cancelled','canceled')
					  AND id < ?
					ORDER BY id ASC LIMIT 1
				`, userID, domainID).Scan(&older)
				if older > 0 {
					_ = DeleteOutreachDomain(domainID, userID)
					return 0, "", fmt.Errorf("your plan already includes one domain — buy an extra domain from Mailboxes, or continue setup for your existing domain")
				}
			}
		}
	}

	resp, err := createInboxKitOrder(inboxkit.CreateOrderRequest{
		Domains: []inboxkit.OrderDomain{{
			Name:              domain,
			RedirectURL:       redirect,
			RegistrationYears: 1,
			Mailboxes:         mboxes,
			ContactDetails:    reg,
		}},
		ContactDetails: reg,
	})
	if err != nil {
		_ = SetOutreachDomainError(domainID, "error", err.Error())
		return 0, "", err
	}
	orderID = resp.ResolvedID()
	if orderID == "" {
		_ = SetOutreachDomainError(domainID, "error", "InboxKit returned an empty order id")
		return 0, "", fmt.Errorf("InboxKit returned an empty order id")
	}
	if uErr := UpdateOutreachDomainStatus(domainID, "ordering", orderID); uErr != nil {
		// Order exists at InboxKit — keep trying to claim the row.
		log.Printf("claim InboxKit order %s for domain %s: %v", orderID, domain, uErr)
		if _, claimErr := CreateOutreachDomain(userID, domain, orderID, redirect, included); claimErr != nil {
			return domainID, orderID, fmt.Errorf("domain was ordered (InboxKit order %s) but could not be saved locally: %v", orderID, claimErr)
		}
	}
	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, included); mbErr != nil {
		log.Printf("upsert starter mailboxes for domain %s: %v", domain, mbErr)
		_ = SetOutreachDomainError(domainID, "ordering", "Order placed but mailbox rows failed to save: "+mbErr.Error())
		return domainID, orderID, fmt.Errorf("order placed but mailbox setup incomplete: %w", mbErr)
	}
	return domainID, orderID, nil
}

// PlaceConnectExistingDomainOrder connects a domain you already own (no registration purchase).
// Step 1: InboxKit creates Cloudflare NS for the domain. Step 2: after NS propagate, mailboxes are bought.
func PlaceConnectExistingDomainOrder(userID int64, domain string, specs []StarterMailboxSpec) (domainID int64, orderID string, nameservers []string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || !strings.Contains(domain, ".") {
		return 0, "", nil, fmt.Errorf("enter a valid domain")
	}
	n := config.InboxKitIncludedMailboxCount()
	if len(specs) == 0 {
		return 0, "", nil, fmt.Errorf("at least one mailbox is required")
	}
	if len(specs) > n {
		specs = specs[:n]
	}

	if config.ManualInboxKitFulfillment {
		return queueConnectDomainOrder(userID, domain, specs)
	}
	if !inboxkit.Configured() {
		return 0, "", nil, fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}

	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}
	mboxes := buildOrderMailboxes(domain, specs, platform)

	if existing, gErr := GetOutreachDomainByName(userID, domain); gErr == nil {
		st := strings.ToLower(existing.Status)
		oid := strings.TrimSpace(existing.InboxkitOrderID)
		if st == "pending_manual" || isManualOrderID(oid) {
			if config.ManualInboxKitFulfillment {
				return queueConnectDomainOrder(userID, domain, specs)
			}
		} else if st != "error" && oid != "" {
			_ = ensureStarterMailboxRows(userID, existing.ID, mboxes, platform, existing.Included)
			return existing.ID, oid, existing.Nameservers(), nil
		}
	}

	if _, gErr := GetOutreachDomainByName(userID, domain); gErr != nil {
		if qErr := assertIncludedDomainQuota(userID); qErr != nil {
			return 0, "", nil, qErr
		}
	}

	ns, err := connectInboxKitNameservers(domain)
	if err != nil {
		return 0, "", nil, fmt.Errorf("connect domain: %w", err)
	}
	nameservers = ns.Nameservers
	orderID = inboxkit.ConnectOrderID(ns.UID, domain)

	domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, true)
	if err != nil {
		return 0, orderID, nameservers, err
	}
	if nActive, cErr := CountActiveIncludedDomains(userID); cErr == nil {
		spec, _ := PlanSpecForTier(PlanTierPro)
		if nActive > spec.IncludedDomains {
			var older int64
			_ = db.QueryRow(`
				SELECT id FROM outreach_domains
				WHERE user_id=? AND included=TRUE
				  AND lower(status) NOT IN ('error','cancelled','canceled')
				  AND id < ?
				ORDER BY id ASC LIMIT 1
			`, userID, domainID).Scan(&older)
			if older > 0 {
				_ = DeleteOutreachDomain(domainID, userID)
				return 0, "", nameservers, fmt.Errorf("your plan already includes one domain — buy an extra domain from Mailboxes, or continue setup for your existing domain")
			}
		}
	}
	_ = UpdateOutreachDomainStatus(domainID, "connecting", orderID)
	if len(nameservers) > 0 {
		_ = SetOutreachDomainNameservers(domainID, nameservers)
	}
	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, true); mbErr != nil {
		_ = SetOutreachDomainError(domainID, "connecting", "Mailbox rows failed to save: "+mbErr.Error())
		return domainID, orderID, nameservers, fmt.Errorf("domain connected but mailbox setup incomplete: %w", mbErr)
	}

	// If NS already propagated (re-connect), try buying mailboxes immediately.
	if check, checkErr := checkInboxKitNameservers(domain); checkErr == nil && (check.Propagated || check.Ready) {
		if buyErr := buyPendingMailboxesForDomain(userID, domainID, domain, platform); buyErr != nil {
			log.Printf("connect domain %s: buy mailboxes after NS ready: %v", domain, buyErr)
			_ = SetOutreachDomainError(domainID, "error", buyErr.Error())
		}
	}

	return domainID, orderID, nameservers, nil
}

func buyPendingMailboxesForDomain(userID, domainID int64, domain, platform string) error {
	mailboxes, err := ListOutreachMailboxes(userID)
	if err != nil {
		return err
	}
	var pending []inboxkit.OrderMailbox
	var pendingLocal []OutreachMailbox
	for _, m := range mailboxes {
		if m.DomainID != domainID {
			continue
		}
		if m.Status == "ready" {
			continue
		}
		// Already bought (real UID or buy placeholder) — never charge again.
		if m.InboxkitMailboxID != "" && !isPendingBuyMailboxID(m.InboxkitMailboxID) {
			continue
		}
		if isPendingBuyMailboxID(m.InboxkitMailboxID) {
			continue
		}
		local := sanitizeLocalPart(strings.Split(m.Email, "@")[0])
		fn, ln := m.FirstName, m.LastName
		if fn == "" {
			fn = "Team"
		}
		if ln == "" {
			ln = "Outreach"
		}
		pending = append(pending, inboxkit.OrderMailbox{
			FirstName: fn,
			LastName:  ln,
			Email:     m.Email,
			Username:  local,
			Platform:  platform,
		})
		pendingLocal = append(pendingLocal, m)
	}
	if len(pending) == 0 {
		return nil
	}
	resp, err := buyInboxKitMailboxes(inboxkit.BuyMailboxesRequest{
		Domain:    domain,
		Mailboxes: inboxkit.BuyItemsFromOrderMailboxes(domain, pending),
	})
	if err != nil {
		return err
	}
	buyOrder := strings.TrimSpace(resp.OrderID)
	if buyOrder == "" {
		buyOrder = strings.TrimSpace(resp.ID)
	}
	if buyOrder == "" {
		buyOrder = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	matched := map[int64]bool{}
	for _, bought := range resp.Mailboxes {
		email := strings.ToLower(bought.Username + "@" + bought.DomainName)
		if bought.Email != "" {
			email = strings.ToLower(bought.Email)
		}
		for _, m := range pendingLocal {
			if matched[m.ID] {
				continue
			}
			if strings.ToLower(m.Email) != email && !strings.EqualFold(sanitizeLocalPart(strings.Split(m.Email, "@")[0]), bought.Username) {
				continue
			}
			uid := strings.TrimSpace(bought.UID)
			if uid == "" {
				uid = strings.TrimSpace(bought.ID)
			}
			if uid == "" {
				uid = pendingBuyIDPrefix + buyOrder + ":" + strings.ToLower(m.Email)
			}
			_ = UpdateOutreachMailboxReady(m.ID, uid, m.SMTPAccountID, "provisioning")
			matched[m.ID] = true
		}
	}
	// Any seat charged but not matched still gets a placeholder so we never rebuy.
	for _, m := range pendingLocal {
		if matched[m.ID] {
			continue
		}
		placeholder := pendingBuyIDPrefix + buyOrder + ":" + strings.ToLower(m.Email)
		_ = UpdateOutreachMailboxReady(m.ID, placeholder, m.SMTPAccountID, "provisioning")
	}
	return nil
}

// SyncInboxKitOrder pulls order/mailbox state and stores SMTP credentials when ready.
func SyncInboxKitOrder(userID, domainID int64) error {
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return err
	}
	if IsManualPendingDomain(d) {
		return nil
	}
	if inboxkit.IsConnectOrderID(d.InboxkitOrderID) || d.Status == "connecting" {
		return syncConnectedDomain(userID, domainID, d)
	}
	if d.InboxkitOrderID == "" || isPendingOrderID(d.InboxkitOrderID) {
		return fmt.Errorf("missing inboxkit order id")
	}
	client := inboxkit.NewClient()
	order, err := client.GetOrder(d.InboxkitOrderID)
	if err != nil {
		_ = SetOutreachDomainError(domainID, d.Status, err.Error())
		return err
	}
	status := order.Status
	if order.IsDone() {
		// Keep non-ready until MarkOutreachDomainReady so we can notify once.
		if status == "" || strings.EqualFold(status, "ready") {
			status = "processing"
		}
	} else if order.IsError() {
		status = "error"
	} else if status == "" {
		status = "processing"
	}
	if !order.IsDone() {
		_ = UpdateOutreachDomainStatus(domainID, status, d.InboxkitOrderID)
	} else {
		_ = UpdateOutreachDomainStatus(domainID, status, d.InboxkitOrderID)
	}
	if order.IsError() {
		_ = SetOutreachDomainError(domainID, "error", "InboxKit order failed")
	}

	if err := syncMailboxCredentials(userID, domainID, d.Domain); err != nil {
		_ = SetOutreachDomainError(domainID, status, err.Error())
		return err
	}

	if order.IsDone() {
		_ = ClearOutreachDomainError(domainID)
		became, _ := MarkOutreachDomainReady(domainID, d.InboxkitOrderID)
		maybeNotifyDomainReady(userID, d.Domain, became)
	} else {
		_ = TouchOutreachDomainSynced(domainID)
	}
	return nil
}

func syncConnectedDomain(userID, domainID int64, d OutreachDomain) error {
	check, err := checkInboxKitNameservers(d.Domain)
	if err != nil {
		log.Printf("sync connected domain %s: check NS: %v", d.Domain, err)
		_ = SetOutreachDomainError(domainID, "connecting", "Nameserver check failed: "+err.Error())
		return nil
	}
	if !check.Propagated && !check.Ready {
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
		_ = TouchOutreachDomainSynced(domainID)
		return nil
	}

	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	if buyErr := buyPendingMailboxesForDomain(userID, domainID, d.Domain, platform); buyErr != nil {
		log.Printf("sync connected domain %s: buy mailboxes: %v", d.Domain, buyErr)
		_ = SetOutreachDomainError(domainID, "error", "Could not buy mailboxes: "+buyErr.Error())
		return buyErr
	}

	if err := syncMailboxCredentials(userID, domainID, d.Domain); err != nil {
		_ = SetOutreachDomainError(domainID, "connecting", err.Error())
		return err
	}
	if UserHasReadyMailbox(userID) {
		_ = ClearOutreachDomainError(domainID)
		became, _ := MarkOutreachDomainReady(domainID, d.InboxkitOrderID)
		maybeNotifyDomainReady(userID, d.Domain, became)
	} else {
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
		_ = TouchOutreachDomainSynced(domainID)
	}
	return nil
}

// SyncPendingOutreachDomains syncs every domain that still needs work for a user.
func SyncPendingOutreachDomains(userID int64) {
	domains, err := ListOutreachDomains(userID)
	if err != nil {
		return
	}
	mailboxes, _ := ListOutreachMailboxes(userID)
	pendingByDomain := map[int64]bool{}
	for _, m := range mailboxes {
		st := strings.ToLower(m.Status)
		if st != "ready" && st != "active" && st != "scheduled_cancel" && st != "scheduled_for_cancellation" {
			pendingByDomain[m.DomainID] = true
		}
	}
	seen := map[int64]bool{}
	for _, d := range domains {
		needs := d.Status != "ready" || d.LastError != "" || pendingByDomain[d.ID]
		if !needs || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		if syncErr := SyncInboxKitOrder(userID, d.ID); syncErr != nil {
			log.Printf("sync pending domain %s (id=%d): %v", d.Domain, d.ID, syncErr)
		}
	}
}

func syncMailboxCredentials(userID, domainID int64, domain string) error {
	client := inboxkit.NewClient()
	list, err := client.ListMailboxes(domain)
	if err != nil {
		return err
	}
	existing, _ := ListOutreachMailboxes(userID)
	byEmail := map[string]OutreachMailbox{}
	for _, m := range existing {
		if m.DomainID == domainID || strings.HasSuffix(strings.ToLower(m.Email), "@"+strings.ToLower(domain)) {
			byEmail[strings.ToLower(m.Email)] = m
		}
	}

	daily := 50
	if u, err := GetUserByID(userID); err == nil {
		if spec, pErr := PlanSpecForTier(NormalizePlanTier(u.PlanTier)); pErr == nil && spec.DailyEmailCap > 0 {
			daily = spec.DailyEmailCap
		}
	}

	var lastCredErr error
	for i, item := range list {
		email := item.ResolvedEmail()
		if email == "" {
			continue
		}
		fwd := item.ResolvedForwarding()
		role := item.Role
		if item.ResolvedIsAdmin() && role == "" {
			role = "Admin"
		}
		statusHint := item.Status
		if item.IsScheduledCancel() {
			statusHint = "scheduled_cancel"
		} else if strings.EqualFold(statusHint, "active") {
			statusHint = "ready"
		}
		local, ok := byEmail[email]
		if !ok {
			id, cErr := UpsertOutreachMailbox(OutreachMailbox{
				UserID:            userID,
				DomainID:          domainID,
				InboxkitMailboxID: item.ResolvedID(),
				Email:             email,
				FirstName:         item.FirstName,
				LastName:          item.LastName,
				Platform:          firstNonEmpty(item.Platform, config.InboxKitPlatform, "GOOGLE"),
				Status:            "provisioning",
				IsDefault:         i == 0 && len(byEmail) == 0,
				IsAdmin:           item.ResolvedIsAdmin(),
				Role:              role,
				ForwardingEmail:   fwd,
				Included:          true,
			})
			if cErr != nil {
				lastCredErr = cErr
				continue
			}
			local, _ = GetOutreachMailbox(id, userID)
			byEmail[email] = local
		} else {
			metaStatus := statusHint
			if local.Status == "ready" && local.SMTPAccountID > 0 {
				metaStatus = "ready"
			}
			_ = SetOutreachMailboxMeta(local.ID, item.ResolvedIsAdmin(), role, fwd, metaStatus)
		}
		mbID := item.ResolvedID()
		if mbID != "" && (local.InboxkitMailboxID == "" || isPendingBuyMailboxID(local.InboxkitMailboxID)) {
			local.InboxkitMailboxID = mbID
			_ = UpdateOutreachMailboxReady(local.ID, mbID, local.SMTPAccountID, local.Status)
		}
		if local.InboxkitMailboxID == "" || isPendingBuyMailboxID(local.InboxkitMailboxID) {
			_ = SetOutreachMailboxError(local.ID, "Waiting for InboxKit mailbox id")
			continue
		}
		if local.Status == "ready" && local.SMTPAccountID > 0 {
			// Still refresh meta / insights; credentials already stored.
			if insights, iErr := client.MailboxInsights(local.InboxkitMailboxID); iErr == nil {
				_ = SetMailboxAnalytics(local.ID, "{}", string(insights))
			}
			continue
		}
		creds, cErr := client.GetMailboxCredentials(local.InboxkitMailboxID)
		if cErr != nil {
			if byEmail, eErr := client.GetMailboxCredentialsByEmail(email); eErr == nil {
				creds = byEmail
				cErr = nil
			}
		}
		if cErr != nil {
			_ = SetOutreachMailboxError(local.ID, "Credentials not ready: "+cErr.Error())
			lastCredErr = cErr
			continue
		}
		pass := creds.ResolvedPassword()
		if pass == "" {
			_ = SetOutreachMailboxError(local.ID, "Credentials returned without a password yet")
			lastCredErr = fmt.Errorf("empty password for %s", email)
			continue
		}
		isDef := local.IsDefault
		smtpID, sErr := UpsertInboxKitSMTPAccount(userID, email, creds.SMTPHost, creds.SMTPPort, creds.ResolvedSMTPUser(), pass, strings.TrimSpace(local.FirstName+" "+local.LastName), local.InboxkitMailboxID, isDef, daily, creds.IMAPHost, creds.IMAPPort)
		if sErr != nil {
			_ = SetOutreachMailboxError(local.ID, sErr.Error())
			return sErr
		}
		_ = UpdateOutreachMailboxReady(local.ID, local.InboxkitMailboxID, smtpID, "ready")
		if insights, iErr := client.MailboxInsights(local.InboxkitMailboxID); iErr == nil {
			_ = SetMailboxAnalytics(local.ID, "{}", string(insights))
		}
	}
	if lastCredErr != nil && !UserHasReadyMailbox(userID) {
		return lastCredErr
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func PlaceExtraMailboxesOrder(userID, domainID int64, specs []StarterMailboxSpec) (orderID string, err error) {
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return "", err
	}
	if d.Status != "ready" && d.Status != "processing" {
		return "", fmt.Errorf("domain %s is still %s — finish domain setup before buying more mailboxes", d.Domain, d.Status)
	}
	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	var mboxes []inboxkit.OrderMailbox
	for _, s := range specs {
		local := sanitizeLocalPart(s.LocalPart)
		if local == "" {
			local = sanitizeLocalPart(s.FirstName + "." + s.LastName)
		}
		if local == "" {
			return "", fmt.Errorf("invalid email local part for %s %s", s.FirstName, s.LastName)
		}
		mboxes = append(mboxes, inboxkit.OrderMailbox{
			FirstName: s.FirstName,
			LastName:  s.LastName,
			Email:     local + "@" + d.Domain,
			Username:  local,
			Platform:  platform,
		})
	}
	if len(mboxes) == 0 {
		return "", fmt.Errorf("add at least one mailbox")
	}
	client := inboxkit.NewClient()
	items := inboxkit.BuyItemsFromOrderMailboxes(d.Domain, mboxes)
	resp, err := client.BuyMailboxes(inboxkit.BuyMailboxesRequest{
		Domain:    d.Domain,
		Mailboxes: items,
	})
	if err != nil {
		return "", err
	}
	orderID = resp.OrderID
	if orderID == "" {
		orderID = resp.ID
	}

	uidByEmail := map[string]string{}
	uidByUser := map[string]string{}
	for _, bought := range resp.Mailboxes {
		uid := bought.UID
		if uid == "" {
			uid = bought.ID
		}
		if uid == "" {
			continue
		}
		if bought.Email != "" {
			uidByEmail[strings.ToLower(bought.Email)] = uid
		}
		user := strings.ToLower(bought.Username)
		if user == "" && bought.Email != "" {
			if i := strings.Index(bought.Email, "@"); i > 0 {
				user = strings.ToLower(bought.Email[:i])
			}
		}
		if user != "" {
			uidByUser[user] = uid
		}
		if bought.DomainName != "" && user != "" {
			uidByEmail[user+"@"+strings.ToLower(bought.DomainName)] = uid
		}
	}

	for _, mb := range mboxes {
		localID, uErr := UpsertOutreachMailbox(OutreachMailbox{
			UserID:    userID,
			DomainID:  domainID,
			Email:     mb.Email,
			FirstName: mb.FirstName,
			LastName:  mb.LastName,
			Platform:  platform,
			Status:    "provisioning",
			Included:  false,
		})
		if uErr != nil {
			log.Printf("upsert mailbox after buy %s: %v", mb.Email, uErr)
			continue
		}
		user := mb.ResolvedUsername()
		uid := uidByEmail[strings.ToLower(mb.Email)]
		if uid == "" {
			uid = uidByUser[user]
		}
		if uid != "" {
			_ = UpdateOutreachMailboxReady(localID, uid, 0, "provisioning")
		}
	}

	// Pull credentials immediately so the list updates without waiting for another page poll.
	if syncErr := syncMailboxCredentials(userID, domainID, d.Domain); syncErr != nil {
		log.Printf("sync credentials after buy on %s: %v", d.Domain, syncErr)
	}
	return orderID, nil
}

// RefreshMailboxFromInboxKit pulls details + credentials for one mailbox.
func RefreshMailboxFromInboxKit(userID, mailboxID int64) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.InboxkitMailboxID == "" {
		return fmt.Errorf("this mailbox is not linked to InboxKit")
	}
	client := inboxkit.NewClient()
	detail, dErr := client.GetMailbox(m.InboxkitMailboxID)
	if dErr == nil {
		role := detail.Role
		if detail.ResolvedIsAdmin() && role == "" {
			role = "Admin"
		}
		status := detail.Status
		if detail.IsScheduledCancel() {
			status = "scheduled_cancel"
		}
		_ = SetOutreachMailboxMeta(m.ID, detail.ResolvedIsAdmin(), role, detail.ResolvedForwarding(), status)
		if detail.FirstName != "" || detail.LastName != "" {
			_, _ = db.Exec(`UPDATE outreach_mailboxes SET first_name=?, last_name=?, updated_at=? WHERE id=?`,
				detail.FirstName, detail.LastName, time.Now(), m.ID)
		}
	}
	creds, cErr := client.GetMailboxCredentials(m.InboxkitMailboxID)
	if cErr != nil {
		_ = SetOutreachMailboxError(m.ID, cErr.Error())
		return cErr
	}
	pass := creds.ResolvedPassword()
	if pass == "" {
		return fmt.Errorf("InboxKit returned empty credentials")
	}
	daily := 50
	if u, err := GetUserByID(userID); err == nil {
		if spec, pErr := PlanSpecForTier(NormalizePlanTier(u.PlanTier)); pErr == nil && spec.DailyEmailCap > 0 {
			daily = spec.DailyEmailCap
		}
	}
	smtpID, sErr := UpsertInboxKitSMTPAccount(userID, m.Email, creds.SMTPHost, creds.SMTPPort, creds.ResolvedSMTPUser(), pass, strings.TrimSpace(m.FirstName+" "+m.LastName), m.InboxkitMailboxID, m.IsDefault, daily, creds.IMAPHost, creds.IMAPPort)
	if sErr != nil {
		return sErr
	}
	_ = UpdateOutreachMailboxReady(m.ID, m.InboxkitMailboxID, smtpID, "ready")
	return nil
}

// RelinkMailboxFromInboxKit finds the InboxKit seat for this email and replaces local SMTP creds
// with the live credentials from InboxKit (fixes broken "manual" copies of Workspace mailboxes).
func RelinkMailboxFromInboxKit(userID, mailboxID int64) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(m.Email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("mailbox email missing")
	}
	if !inboxkit.Configured() {
		return fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}
	domain := email[strings.Index(email, "@")+1:]
	client := inboxkit.NewClient()
	list, err := client.ListMailboxes(domain)
	if err != nil {
		return fmt.Errorf("list InboxKit mailboxes: %w", err)
	}
	var mbID string
	for _, item := range list {
		if strings.EqualFold(item.ResolvedEmail(), email) {
			mbID = item.ResolvedID()
			break
		}
	}
	if mbID == "" && m.InboxkitMailboxID != "" {
		mbID = m.InboxkitMailboxID
	}
	if mbID == "" {
		return fmt.Errorf("InboxKit has no mailbox %s — attach with the password InboxKit shows for that seat, or finish domain provisioning", email)
	}
	_ = UpdateOutreachMailboxReady(m.ID, mbID, m.SMTPAccountID, m.Status)
	m.InboxkitMailboxID = mbID
	return RefreshMailboxFromInboxKit(userID, m.ID)
}

// ApplySharedSMTPCredentialsToMailbox copies the server Free/shared SMTP env credentials onto this mailbox.
// Use when the seat should send as the same address configured in SMTP_USER / APP_PASSWORD.
func ApplySharedSMTPCredentialsToMailbox(userID, mailboxID int64) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if config.SMTPHost == "" || config.SMTPUser == "" || config.SMTPPass == "" {
		return fmt.Errorf("shared SMTP is not configured on the server")
	}
	email := strings.ToLower(strings.TrimSpace(m.Email))
	if !strings.EqualFold(config.SMTPUser, email) && !strings.EqualFold(config.SMTPFrom, email) {
		return fmt.Errorf("server shared SMTP is %s, not %s — cannot copy those credentials onto this mailbox", config.SMTPUser, m.Email)
	}
	port := config.SMTPPort
	if port == "" {
		port = "587"
	}
	host := config.SMTPHost
	pass := config.SMTPPass
	user := config.SMTPUser
	return UpdateMailboxCredentials(userID, mailboxID, host, port, config.SharedIMAPHost(), config.SharedIMAPPort(), user, pass)
}

// AttachMailboxSmart attaches SMTP for an address. Prefer live InboxKit credentials when the
// address belongs to an InboxKit domain; otherwise store the form password as manual.
func AttachMailboxSmart(userID int64, email, fromName, smtpHost, smtpPort, imapHost, imapPort, username, password string, isDefault bool) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if inboxkit.Configured() && strings.Contains(email, "@") {
		domain := email[strings.Index(email, "@")+1:]
		client := inboxkit.NewClient()
		if list, err := client.ListMailboxes(domain); err == nil {
			for _, item := range list {
				if !strings.EqualFold(item.ResolvedEmail(), email) {
					continue
				}
				mbID := item.ResolvedID()
				if mbID == "" {
					break
				}
				creds, cErr := client.GetMailboxCredentials(mbID)
				if cErr != nil {
					return 0, fmt.Errorf("InboxKit credentials for %s: %w", email, cErr)
				}
				pass := creds.ResolvedPassword()
				if pass == "" {
					return 0, fmt.Errorf("InboxKit returned empty credentials for %s", email)
				}
				host := creds.SMTPHost
				if host == "" {
					host = smtpHost
				}
				if host == "" {
					host = "smtp.gmail.com"
				}
				port := normalizeGmailSMTPPort(host, firstNonEmpty(creds.SMTPPort, smtpPort, "587"))
				user := creds.ResolvedSMTPUser()
				if user == "" {
					user = email
				}
				imapH := firstNonEmpty(creds.IMAPHost, imapHost, "imap.gmail.com")
				imapP := firstNonEmpty(creds.IMAPPort, imapPort, "993")
				daily := 50
				if spec, err := PlanSpecForTier(PlanTierPro); err == nil && spec.DailyEmailCap > 0 {
					daily = spec.DailyEmailCap
				}
				if fromName == "" {
					fromName = strings.TrimSpace(item.FirstName + " " + item.LastName)
				}
				if fromName == "" {
					fromName = email
				}
				smtpID, err := UpsertInboxKitSMTPAccount(userID, email, host, port, user, pass, fromName, mbID, isDefault, daily, imapH, imapP)
				if err != nil {
					return 0, err
				}
				domainID := int64(0)
				if d, dErr := GetOutreachDomainByName(userID, domain); dErr == nil {
					domainID = d.ID
				}
				oid, err := UpsertOutreachMailbox(OutreachMailbox{
					UserID:            userID,
					DomainID:          domainID,
					SMTPAccountID:     smtpID,
					InboxkitMailboxID: mbID,
					Email:             email,
					FirstName:         item.FirstName,
					LastName:          item.LastName,
					Platform:          firstNonEmpty(item.Platform, "GOOGLE"),
					Status:            "ready",
					IsDefault:         isDefault,
					Included:          false,
				})
				if err != nil {
					return 0, err
				}
				_ = UpdateOutreachMailboxReady(oid, mbID, smtpID, "ready")
				if isDefault {
					_ = SetDefaultOutreachMailbox(userID, oid)
				}
				return oid, nil
			}
		}
	}
	if username == "" {
		username = email
	}
	if smtpHost == "" {
		smtpHost = "smtp.gmail.com"
	}
	if smtpPort == "" {
		smtpPort = "587"
	}
	return AttachManualSendingMailbox(userID, email, fromName, smtpHost, smtpPort, imapHost, imapPort, username, password, isDefault)
}

// CancelInboxKitMailbox cancels the seat at InboxKit and marks local status.
func CancelInboxKitMailbox(userID, mailboxID int64) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.InboxkitMailboxID == "" {
		return fmt.Errorf("this mailbox is not linked to InboxKit — delete it locally instead")
	}
	client := inboxkit.NewClient()
	if err := client.CancelMailboxes([]string{m.InboxkitMailboxID}); err != nil {
		return err
	}
	return MarkOutreachMailboxCancelled(m.ID)
}

// SetMailboxForwarding configures InboxKit forwarding for a mailbox.
func SetMailboxForwarding(userID, mailboxID int64, forwardingEmail string, remove bool) error {
	m, err := GetOutreachMailbox(mailboxID, userID)
	if err != nil {
		return err
	}
	if m.InboxkitMailboxID == "" {
		return fmt.Errorf("forwarding requires an InboxKit mailbox")
	}
	client := inboxkit.NewClient()
	uids := []string{m.InboxkitMailboxID}
	if remove || strings.TrimSpace(forwardingEmail) == "" {
		if _, err := client.RemoveForwarding(uids); err != nil {
			return err
		}
		return SetOutreachMailboxForwarding(m.ID, "")
	}
	forwardingEmail = strings.TrimSpace(forwardingEmail)
	var opErr error
	if strings.TrimSpace(m.ForwardingEmail) != "" {
		_, opErr = client.UpdateForwarding(uids, forwardingEmail)
	} else {
		_, opErr = client.SetupForwarding(uids, forwardingEmail)
	}
	if opErr != nil {
		return opErr
	}
	return SetOutreachMailboxForwarding(m.ID, forwardingEmail)
}

func sanitizeLocalPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	out = strings.Trim(out, ".")
	return out
}

func ParseMailboxSpecsJSON(raw string) ([]StarterMailboxSpec, error) {
	var specs []StarterMailboxSpec
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		return nil, err
	}
	return specs, nil
}

type DomainPurchasePayload struct {
	Kind      string               `json:"kind"`
	Domain    string               `json:"domain"`
	Mailboxes []StarterMailboxSpec `json:"mailboxes"`
}

func ParseDomainPurchasePayload(raw string) (DomainPurchasePayload, error) {
	var p DomainPurchasePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return DomainPurchasePayload{}, err
	}
	return p, nil
}
