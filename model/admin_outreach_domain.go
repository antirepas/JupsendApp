package model

import (
	"fmt"
	"log"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/inboxkit"
)

// EnsureAdminOutreachDomain links config.AdminOutreachDomain to an admin user
// when the domain is already (or can be) connected in the InboxKit workspace.
// It syncs existing seats into Mailboxes; it does not buy new ones.
func EnsureAdminOutreachDomain(userID int64) error {
	domain := strings.ToLower(strings.TrimSpace(config.AdminOutreachDomain))
	if domain == "" || !strings.Contains(domain, ".") {
		return nil
	}
	u, err := GetUserByID(userID)
	if err != nil || !UserIsAdmin(u) {
		return nil
	}
	if !inboxkit.Configured() {
		return fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}

	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}

	existing, gErr := GetOutreachDomainByName(userID, domain)
	var domainID int64
	if gErr == nil {
		domainID = existing.ID
	}

	ns, cErr := connectInboxKitNameservers(domain)
	if cErr != nil {
		if domainID > 0 {
			// Still try syncing seats if the domain row already exists.
			if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
				log.Printf("admin domain sync %s: %v", domain, syncErr)
			}
		}
		return fmt.Errorf("link admin domain %s: %w", domain, cErr)
	}

	orderID := inboxkit.ConnectOrderID(ns.UID, domain)
	if orderID == "" || ns.UID == "" {
		orderID = inboxkit.ConnectOrderID("admin-linked", domain)
	}

	if domainID == 0 {
		domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, true)
		if err != nil {
			return fmt.Errorf("create admin domain row: %w", err)
		}
	}

	status := "ready"
	if !ns.Ready && !ns.Propagated {
		check, checkErr := checkInboxKitNameservers(domain)
		if checkErr != nil || !(check.Propagated || check.Ready) {
			status = "connecting"
		}
	}
	_ = UpdateOutreachDomainStatus(domainID, status, orderID)
	if len(ns.Nameservers) > 0 {
		_ = SetOutreachDomainNameservers(domainID, ns.Nameservers)
	}
	_ = ClearOutreachDomainError(domainID)

	if syncErr := syncMailboxCredentials(userID, domainID, domain); syncErr != nil {
		log.Printf("admin domain sync %s: %v", domain, syncErr)
	}
	return nil
}
