package model

import (
	"fmt"
	"log"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/inboxkit"
)

// AdminConnectDomainWithMailboxes is an ops repair path: connect a customer's domain
// to InboxKit (if needed) and buy/sync up to the included mailbox seats.
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
		if qErr := assertIncludedDomainQuota(userID); qErr != nil {
			return 0, "", qErr
		}
		domainID, err = CreateOutreachDomain(userID, domain, "", redirect, true)
		if err != nil {
			return 0, "", err
		}
	}

	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, true); mbErr != nil {
		return domainID, "", fmt.Errorf("save mailbox rows: %w", mbErr)
	}

	oid := ""
	if gErr == nil {
		oid = strings.TrimSpace(existing.InboxkitOrderID)
	}
	needsConnect := oid == "" || isManualOrderID(oid) || isPendingOrderID(oid)
	if !needsConnect {
		st := ""
		if gErr == nil {
			st = strings.ToLower(existing.Status)
		}
		if st == "error" || st == "cancelled" || st == "canceled" {
			needsConnect = true
		}
	}

	var nameservers []string
	if needsConnect {
		ns, cErr := connectInboxKitNameservers(domain)
		if cErr != nil {
			_ = SetOutreachDomainError(domainID, "error", cErr.Error())
			return domainID, "", fmt.Errorf("connect domain nameservers: %w", cErr)
		}
		nameservers = ns.Nameservers
		orderID := inboxkit.ConnectOrderID(ns.UID, domain)
		_ = UpdateOutreachDomainStatus(domainID, "connecting", orderID)
		if len(nameservers) > 0 {
			_ = SetOutreachDomainNameservers(domainID, nameservers)
		}
		oid = orderID
	} else if gErr == nil {
		nameservers = existing.Nameservers()
	}

	// Prefer importing seats already present in InboxKit.
	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		log.Printf("admin provision sync %s: %v", domain, syncErr)
	}
	ready, _ := countDomainReadyMailboxes(userID, domainID)
	if ready >= len(specs) {
		_ = UpdateOutreachDomainStatus(domainID, "ready", oid)
		return domainID, fmt.Sprintf("Synced %d mailbox(es) already on InboxKit for %s", ready, domain), nil
	}

	check, checkErr := checkInboxKitNameservers(domain)
	nsReady := checkErr == nil && (check.Propagated || check.Ready)
	if !nsReady {
		nsHint := strings.Join(nameservers, ", ")
		if nsHint == "" {
			nsHint = "(see InboxKit / domain status page)"
		}
		_ = UpdateOutreachDomainStatus(domainID, "connecting", oid)
		return domainID, fmt.Sprintf(
			"Domain %s is connecting. Point nameservers to: %s — then re-run this form or open Mailboxes to finish buying seats.",
			domain, nsHint,
		), nil
	}

	if buyErr := buyPendingMailboxesForDomain(userID, domainID, domain, platform); buyErr != nil {
		_ = SetOutreachDomainError(domainID, "error", buyErr.Error())
		return domainID, "", fmt.Errorf("buy mailboxes: %w", buyErr)
	}
	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		log.Printf("admin provision sync after buy %s: %v", domain, syncErr)
	}
	ready, _ = countDomainReadyMailboxes(userID, domainID)
	_ = UpdateOutreachDomainStatus(domainID, "ready", oid)
	return domainID, fmt.Sprintf("Provisioned %s — %d mailbox(es) linked (InboxKit wallet charged for any new seats)", domain, ready), nil
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
