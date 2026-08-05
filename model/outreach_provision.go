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

// PlaceStarterDomainOrder buys a domain + included mailboxes via InboxKit.
func PlaceStarterDomainOrder(userID int64, domain string, specs []StarterMailboxSpec) (domainID int64, orderID string, err error) {
	if !inboxkit.Configured() {
		return 0, "", fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}
	n := config.InboxKitIncludedMailboxCount()
	if len(specs) == 0 {
		return 0, "", fmt.Errorf("at least one mailbox is required")
	}
	if len(specs) > n {
		specs = specs[:n]
	}
	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}

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

	client := inboxkit.NewClient()
	resp, err := client.CreateOrder(inboxkit.CreateOrderRequest{
		Domains: []inboxkit.OrderDomain{{
			Name:              domain,
			RedirectURL:       redirect,
			RegistrationYears: 1,
			Mailboxes:         mboxes,
			ContactDetails:    inboxkit.DefaultRegistrant(),
		}},
		ContactDetails: inboxkit.DefaultRegistrant(),
	})
	if err != nil {
		return 0, "", err
	}
	orderID = resp.ResolvedID()
	domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, true)
	if err != nil {
		return 0, orderID, err
	}
	for i, mb := range mboxes {
		isDef := i == 0
		_, _ = UpsertOutreachMailbox(OutreachMailbox{
			UserID:    userID,
			DomainID:  domainID,
			Email:     mb.Email,
			FirstName: mb.FirstName,
			LastName:  mb.LastName,
			Platform:  platform,
			Status:    "provisioning",
			IsDefault: isDef,
			Included:  true,
		})
	}
	return domainID, orderID, nil
}

// PlaceConnectExistingDomainOrder connects a domain you already own (no registration purchase).
// Step 1: InboxKit creates Cloudflare NS for the domain. Step 2: after NS propagate, mailboxes are bought.
func PlaceConnectExistingDomainOrder(userID int64, domain string, specs []StarterMailboxSpec) (domainID int64, orderID string, nameservers []string, err error) {
	if !inboxkit.Configured() {
		return 0, "", nil, fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}
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
	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}

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

	client := inboxkit.NewClient()
	ns, err := client.ConnectDomainNameservers(domain)
	if err != nil {
		return 0, "", nil, fmt.Errorf("connect domain: %w", err)
	}
	nameservers = ns.Nameservers
	orderID = inboxkit.ConnectOrderID(ns.UID, domain)

	domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, true)
	if err != nil {
		return 0, orderID, nameservers, err
	}
	_ = UpdateOutreachDomainStatus(domainID, "connecting", orderID)
	if len(nameservers) > 0 {
		_ = SetOutreachDomainNameservers(domainID, nameservers)
	}
	for i, mb := range mboxes {
		isDef := i == 0
		_, _ = UpsertOutreachMailbox(OutreachMailbox{
			UserID:    userID,
			DomainID:  domainID,
			Email:     mb.Email,
			FirstName: mb.FirstName,
			LastName:  mb.LastName,
			Platform:  platform,
			Status:    "provisioning",
			IsDefault: isDef,
			Included:  true,
		})
	}

	// If NS already propagated (re-connect), try buying mailboxes immediately.
	if check, checkErr := client.CheckNameservers(domain); checkErr == nil && (check.Propagated || check.Ready) {
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
	for _, m := range mailboxes {
		if m.DomainID != domainID {
			continue
		}
		if m.InboxkitMailboxID != "" || m.Status == "ready" {
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
	}
	if len(pending) == 0 {
		return nil
	}
	client := inboxkit.NewClient()
	resp, err := client.BuyMailboxes(inboxkit.BuyMailboxesRequest{
		Domain:    domain,
		Mailboxes: inboxkit.BuyItemsFromOrderMailboxes(domain, pending),
	})
	if err != nil {
		return err
	}
	for _, bought := range resp.Mailboxes {
		email := strings.ToLower(bought.Username + "@" + bought.DomainName)
		for _, m := range mailboxes {
			if m.DomainID != domainID {
				continue
			}
			if strings.ToLower(m.Email) != email && !strings.EqualFold(sanitizeLocalPart(strings.Split(m.Email, "@")[0]), bought.Username) {
				continue
			}
			if bought.UID != "" {
				_ = UpdateOutreachMailboxReady(m.ID, bought.UID, m.SMTPAccountID, "provisioning")
			}
		}
	}
	return nil
}

// SyncInboxKitOrder pulls order/mailbox state and stores SMTP credentials when ready.
func SyncInboxKitOrder(userID, domainID int64) error {
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return err
	}
	if inboxkit.IsConnectOrderID(d.InboxkitOrderID) || d.Status == "connecting" {
		return syncConnectedDomain(userID, domainID, d)
	}
	if d.InboxkitOrderID == "" {
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
		status = "ready"
	} else if order.IsError() {
		status = "error"
	} else if status == "" {
		status = "processing"
	}
	_ = UpdateOutreachDomainStatus(domainID, status, d.InboxkitOrderID)
	if order.IsError() {
		_ = SetOutreachDomainError(domainID, "error", "InboxKit order failed")
	}

	if err := syncMailboxCredentials(userID, domainID, d.Domain); err != nil {
		_ = SetOutreachDomainError(domainID, status, err.Error())
		return err
	}

	if order.IsDone() {
		_ = ClearOutreachDomainError(domainID)
		_ = UpdateOutreachDomainStatus(domainID, "ready", d.InboxkitOrderID)
	} else {
		_ = TouchOutreachDomainSynced(domainID)
	}
	return nil
}

func syncConnectedDomain(userID, domainID int64, d OutreachDomain) error {
	client := inboxkit.NewClient()
	check, err := client.CheckNameservers(d.Domain)
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
		_ = UpdateOutreachDomainStatus(domainID, "ready", d.InboxkitOrderID)
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
		email := strings.ToLower(strings.TrimSpace(item.Email))
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
			_ = SetOutreachMailboxMeta(local.ID, item.ResolvedIsAdmin(), role, fwd, statusHint)
		}
		mbID := item.ResolvedID()
		if mbID != "" && local.InboxkitMailboxID == "" {
			local.InboxkitMailboxID = mbID
		}
		if local.InboxkitMailboxID == "" {
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
