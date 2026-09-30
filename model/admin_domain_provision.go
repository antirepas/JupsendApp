package model

import (
	"fmt"
	"log"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/inboxkit"
)

// AdminConnectDomainWithMailboxes is an ops repair path: link a customer's domain
// that may already exist in InboxKit, then buy/sync the requested mailbox seats.
// It always talks to InboxKit immediately (does not queue for manual fulfillment).
func AdminConnectDomainWithMailboxes(userID int64, domain string, specs []StarterMailboxSpec) (domainID int64, detail string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || !strings.Contains(domain, ".") {
		return 0, "", fmt.Errorf("enter a valid domain")
	}
	if userID <= 0 {
		return 0, "", fmt.Errorf("user is required")
	}
	if !inboxkit.Configured() {
		return 0, "", fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}
	n := config.InboxKitIncludedMailboxCount()
	if len(specs) == 0 {
		return 0, "", fmt.Errorf("add at least one mailbox (first name, last name, local part)")
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
	mboxes := buildOrderMailboxes(domain, specs, platform)

	existing, gErr := GetOutreachDomainByName(userID, domain)
	if gErr == nil {
		domainID = existing.ID
	} else {
		domainID, err = CreateOutreachDomain(userID, domain, "", redirect, false)
		if err != nil {
			return 0, "", err
		}
	}

	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, false); mbErr != nil {
		return domainID, "", fmt.Errorf("save mailbox rows: %w", mbErr)
	}

	// Always reconcile with InboxKit connect endpoint (409 = already in workspace is OK).
	ns, cErr := connectInboxKitNameservers(domain)
	if cErr != nil {
		_ = SetOutreachDomainError(domainID, "error", cErr.Error())
		return domainID, "", fmt.Errorf("connect domain nameservers: %w", cErr)
	}
	orderID := inboxkit.ConnectOrderID(ns.UID, domain)
	if orderID == "" || ns.UID == "" {
		orderID = inboxkit.ConnectOrderID("linked", domain)
	}
	status := "connecting"
	if ns.Ready || ns.Propagated {
		status = "ready"
	}
	_ = UpdateOutreachDomainStatus(domainID, status, orderID)
	if len(ns.Nameservers) > 0 {
		_ = SetOutreachDomainNameservers(domainID, ns.Nameservers)
	}

	// Import any seats already on InboxKit for this domain.
	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		log.Printf("admin provision sync %s: %v", domain, syncErr)
	}

	pending := countPendingBuyMailboxes(userID, domainID)
	if pending == 0 {
		ready, _ := countDomainReadyMailboxes(userID, domainID)
		_ = UpdateOutreachDomainStatus(domainID, "ready", orderID)
		return domainID, fmt.Sprintf("Linked %s — %d mailbox(es) ready (requested seats already present)", domain, ready), nil
	}

	check, checkErr := checkInboxKitNameservers(domain)
	nsReady := ns.Ready || ns.Propagated || (checkErr == nil && (check.Propagated || check.Ready))
	if !nsReady {
		nsHint := strings.Join(ns.Nameservers, ", ")
		if nsHint == "" {
			nsHint = "(see InboxKit)"
		}
		_ = UpdateOutreachDomainStatus(domainID, "connecting", orderID)
		return domainID, fmt.Sprintf(
			"Linked %s in jupsend. Nameservers not ready yet (%s). Re-run after DNS propagates to buy the %d pending seat(s).",
			domain, nsHint, pending,
		), nil
	}

	if buyErr := buyPendingMailboxesForDomain(userID, domainID, domain, platform); buyErr != nil {
		_ = SetOutreachDomainError(domainID, "error", buyErr.Error())
		return domainID, "", fmt.Errorf("buy mailboxes: %w", buyErr)
	}
	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		log.Printf("admin provision sync after buy %s: %v", domain, syncErr)
	}
	ready, _ := countDomainReadyMailboxes(userID, domainID)
	_ = UpdateOutreachDomainStatus(domainID, "ready", orderID)
	return domainID, fmt.Sprintf("Linked %s — %d mailbox(es) linked (InboxKit wallet charged for new seats)", domain, ready), nil
}

// AdminImportExistingInboxKitDomain links a domain already in the InboxKit workspace and
// imports every seat InboxKit lists for it — no mailbox purchase. Use this when seats
// were created manually (e.g. included free mailboxes) and only need to appear in jupsend.
func AdminImportExistingInboxKitDomain(userID int64, domain string) (domainID int64, detail string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || !strings.Contains(domain, ".") {
		return 0, "", fmt.Errorf("enter a valid domain")
	}
	if userID <= 0 {
		return 0, "", fmt.Errorf("user is required")
	}
	if !inboxkit.Configured() {
		return 0, "", fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}

	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}

	existing, gErr := GetOutreachDomainByName(userID, domain)
	if gErr == nil {
		domainID = existing.ID
	} else {
		domainID, err = CreateOutreachDomain(userID, domain, "", redirect, false)
		if err != nil {
			return 0, "", err
		}
	}

	ns, cErr := connectInboxKitNameservers(domain)
	if cErr != nil {
		// Domain may already be connected; still try listing mailboxes.
		log.Printf("admin import connect %s: %v (continuing with list/sync)", domain, cErr)
	}
	orderID := ""
	if cErr == nil {
		orderID = inboxkit.ConnectOrderID(ns.UID, domain)
		if orderID == "" || ns.UID == "" {
			orderID = inboxkit.ConnectOrderID("linked", domain)
		}
		status := "connecting"
		if ns.Ready || ns.Propagated {
			status = "ready"
		}
		_ = UpdateOutreachDomainStatus(domainID, status, orderID)
		if len(ns.Nameservers) > 0 {
			_ = SetOutreachDomainNameservers(domainID, ns.Nameservers)
		}
		_ = ClearOutreachDomainError(domainID)
	} else if orderID == "" {
		orderID = inboxkit.ConnectOrderID("import", domain)
		_ = UpdateOutreachDomainStatus(domainID, "ready", orderID)
	}

	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		_ = SetOutreachDomainError(domainID, "error", syncErr.Error())
		return domainID, "", fmt.Errorf("import mailboxes from InboxKit: %w", syncErr)
	}

	ready, _ := countDomainReadyMailboxes(userID, domainID)
	total := 0
	if mboxes, lErr := ListOutreachMailboxes(userID); lErr == nil {
		for _, m := range mboxes {
			if m.DomainID == domainID {
				total++
			}
		}
	}
	if ready > 0 {
		_ = UpdateOutreachDomainStatus(domainID, "ready", orderID)
	}
	if total == 0 {
		return domainID, "", fmt.Errorf("linked %s but InboxKit returned no mailboxes — check the domain name matches InboxKit exactly", domain)
	}
	return domainID, fmt.Sprintf("Imported %s — %d mailbox(es) on domain (%d send-ready). No seats were purchased.", domain, total, ready), nil
}

func countDomainReadyMailboxes(userID, domainID int64) (int, error) {
	mailboxes, err := ListOutreachMailboxes(userID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range mailboxes {
		if m.DomainID != domainID {
			continue
		}
		if m.Status == "ready" || (m.SMTPAccountID > 0 && m.InboxkitMailboxID != "" && !isPendingBuyMailboxID(m.InboxkitMailboxID)) {
			n++
		}
	}
	return n, nil
}

func countPendingBuyMailboxes(userID, domainID int64) int {
	mailboxes, err := ListOutreachMailboxes(userID)
	if err != nil {
		return 0
	}
	n := 0
	for _, m := range mailboxes {
		if m.DomainID != domainID {
			continue
		}
		if m.Status == "ready" {
			continue
		}
		if m.InboxkitMailboxID != "" && !isPendingBuyMailboxID(m.InboxkitMailboxID) {
			continue
		}
		if isPendingBuyMailboxID(m.InboxkitMailboxID) {
			continue
		}
		n++
	}
	return n
}
