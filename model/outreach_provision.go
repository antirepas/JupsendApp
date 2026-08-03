package model

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"emailtracker.com/config"
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
		_, _ = CreateOutreachMailbox(OutreachMailbox{
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
		_, _ = CreateOutreachMailbox(OutreachMailbox{
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

	if err := syncMailboxCredentials(userID, domainID, d.Domain); err != nil {
		return err
	}

	if order.IsDone() {
		_ = UpdateOutreachDomainStatus(domainID, "ready", d.InboxkitOrderID)
	}
	return nil
}

func syncConnectedDomain(userID, domainID int64, d OutreachDomain) error {
	client := inboxkit.NewClient()
	check, err := client.CheckNameservers(d.Domain)
	if err != nil {
		log.Printf("sync connected domain %s: check NS: %v", d.Domain, err)
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
		return nil
	}
	if !check.Propagated && !check.Ready {
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
		return nil
	}

	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	if buyErr := buyPendingMailboxesForDomain(userID, domainID, d.Domain, platform); buyErr != nil {
		log.Printf("sync connected domain %s: buy mailboxes: %v", d.Domain, buyErr)
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
		// Keep trying on later polls; domain may still be warming up.
	}

	if err := syncMailboxCredentials(userID, domainID, d.Domain); err != nil {
		return err
	}
	if UserHasReadyMailbox(userID) {
		_ = UpdateOutreachDomainStatus(domainID, "ready", d.InboxkitOrderID)
	} else {
		_ = UpdateOutreachDomainStatus(domainID, "connecting", d.InboxkitOrderID)
	}
	return nil
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

	for i, item := range list {
		email := strings.ToLower(item.Email)
		if email == "" && item.Domain != "" {
			// Some list payloads may omit email; skip until we can match.
			continue
		}
		local, ok := byEmail[email]
		if !ok {
			id, cErr := CreateOutreachMailbox(OutreachMailbox{
				UserID:            userID,
				DomainID:          domainID,
				InboxkitMailboxID: item.ResolvedID(),
				Email:             item.Email,
				Platform:          config.InboxKitPlatform,
				Status:            "provisioning",
				IsDefault:         i == 0 && len(byEmail) == 0,
				Included:          true,
			})
			if cErr != nil {
				continue
			}
			local, _ = GetOutreachMailbox(id, userID)
		}
		mbID := item.ResolvedID()
		if mbID != "" && local.InboxkitMailboxID == "" {
			local.InboxkitMailboxID = mbID
		}
		if local.InboxkitMailboxID == "" {
			continue
		}
		creds, cErr := client.GetMailboxCredentials(local.InboxkitMailboxID)
		if cErr != nil {
			continue
		}
		pass := creds.ResolvedPassword()
		if pass == "" {
			continue
		}
		isDef := local.IsDefault
		smtpID, sErr := UpsertInboxKitSMTPAccount(userID, item.Email, creds.SMTPHost, creds.SMTPPort, creds.ResolvedSMTPUser(), pass, local.FirstName+" "+local.LastName, local.InboxkitMailboxID, isDef, daily, creds.IMAPHost, creds.IMAPPort)
		if sErr != nil {
			return sErr
		}
		_ = UpdateOutreachMailboxReady(local.ID, local.InboxkitMailboxID, smtpID, "ready")
		if insights, iErr := client.MailboxInsights(local.InboxkitMailboxID); iErr == nil {
			_ = SetMailboxAnalytics(local.ID, "{}", string(insights))
		}
	}
	return nil
}

func PlaceExtraMailboxesOrder(userID, domainID int64, specs []StarterMailboxSpec) (orderID string, err error) {
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return "", err
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
		mboxes = append(mboxes, inboxkit.OrderMailbox{
			FirstName: s.FirstName,
			LastName:  s.LastName,
			Email:     local + "@" + d.Domain,
			Username:  local,
			Platform:  platform,
		})
	}
	client := inboxkit.NewClient()
	resp, err := client.BuyMailboxes(inboxkit.BuyMailboxesRequest{
		Mailboxes: inboxkit.BuyItemsFromOrderMailboxes(d.Domain, mboxes),
	})
	if err != nil {
		return "", err
	}
	orderID = resp.OrderID
	if orderID == "" {
		orderID = resp.ID
	}
	for _, mb := range mboxes {
		_, _ = CreateOutreachMailbox(OutreachMailbox{
			UserID:    userID,
			DomainID:  domainID,
			Email:     mb.Email,
			FirstName: mb.FirstName,
			LastName:  mb.LastName,
			Platform:  platform,
			Status:    "provisioning",
			Included:  false,
		})
	}
	return orderID, nil
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
