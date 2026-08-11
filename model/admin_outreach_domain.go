package model

import (
	"fmt"
	"log"
	"strings"
	"unicode"

	"emailtracker.com/config"
	"emailtracker.com/inboxkit"
)

// EnsureAdminOutreachDomain links config.AdminOutreachDomain to an admin user
// when the domain is already (or can be) connected in the InboxKit workspace.
// If ADMIN_OUTREACH_MAILBOXES is set, only those seats are imported/kept (others on
// that domain are removed). Skips InboxKit entirely when already linked and ready.
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

	specs := ParseAdminOutreachMailboxSpecs(config.AdminOutreachMailboxes, domain)
	allow := adminMailboxAllowset(specs, domain)

	existing, gErr := GetOutreachDomainByName(userID, domain)
	var domainID int64
	if gErr == nil {
		domainID = existing.ID
	}

	// Fast path: domain linked and allowlisted seats already send-ready — no InboxKit round-trips.
	if domainID > 0 && existing.Status == "ready" && existing.LastError == "" {
		_ = pruneAdminDomainMailboxes(userID, domainID, domain, allow)
		if adminAllowlistReady(userID, domainID, allow) {
			return nil
		}
	}

	redirect := config.InboxKitRedirectURL
	if redirect == "" {
		redirect = config.BaseURL
	}

	ns, cErr := connectInboxKitNameservers(domain)
	if cErr != nil {
		if domainID > 0 {
			_ = pruneAdminDomainMailboxes(userID, domainID, domain, allow)
			if syncErr := syncMailboxCredentialsFiltered(userID, domainID, domain, allow, false); syncErr != nil {
				log.Printf("admin domain sync %s: %v", domain, syncErr)
			}
			_ = ensureAdminOutreachMailboxes(userID, domainID, domain, specs, allow)
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

	_ = pruneAdminDomainMailboxes(userID, domainID, domain, allow)
	if err := ensureAdminOutreachMailboxes(userID, domainID, domain, specs, allow); err != nil {
		log.Printf("admin outreach mailboxes %s: %v", domain, err)
		return err
	}
	return nil
}

// PruneAdminOutreachExtras deletes seats on ADMIN_OUTREACH_DOMAIN that are not in
// ADMIN_OUTREACH_MAILBOXES. Local DB only — safe to call on every page load.
func PruneAdminOutreachExtras(userID int64) error {
	domain := strings.ToLower(strings.TrimSpace(config.AdminOutreachDomain))
	if domain == "" || !strings.Contains(domain, ".") {
		return nil
	}
	u, err := GetUserByID(userID)
	if err != nil || !UserIsAdmin(u) {
		return nil
	}
	specs := ParseAdminOutreachMailboxSpecs(config.AdminOutreachMailboxes, domain)
	allow := adminMailboxAllowset(specs, domain)
	if len(allow) == 0 {
		return nil
	}
	existing, gErr := GetOutreachDomainByName(userID, domain)
	if gErr != nil {
		return nil
	}
	return pruneAdminDomainMailboxes(userID, existing.ID, domain, allow)
}

func adminMailboxAllowset(specs []StarterMailboxSpec, domain string) map[string]bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if len(specs) == 0 {
		return nil // nil = no allowlist (legacy: sync whatever exists locally needing creds only)
	}
	out := make(map[string]bool, len(specs))
	for _, s := range specs {
		local := sanitizeLocalPart(s.LocalPart)
		if local == "" || domain == "" {
			continue
		}
		out[local+"@"+domain] = true
	}
	return out
}

func adminAllowlistReady(userID, domainID int64, allow map[string]bool) bool {
	if len(allow) == 0 {
		return true
	}
	existing, _ := ListOutreachMailboxes(userID)
	ready := map[string]bool{}
	for _, m := range existing {
		if m.DomainID != domainID {
			continue
		}
		email := strings.ToLower(m.Email)
		if (m.Status == "ready" || m.Status == "active") && m.SMTPAccountID > 0 {
			ready[email] = true
		}
	}
	for email := range allow {
		if !ready[email] {
			return false
		}
	}
	return true
}

// pruneAdminDomainMailboxes deletes seats on the admin domain that are not in the allowlist.
func pruneAdminDomainMailboxes(userID, domainID int64, domain string, allow map[string]bool) error {
	if len(allow) == 0 {
		return nil
	}
	existing, err := ListOutreachMailboxes(userID)
	if err != nil {
		return err
	}
	suffix := "@" + strings.ToLower(domain)
	for _, m := range existing {
		if m.DomainID != domainID && !strings.HasSuffix(strings.ToLower(m.Email), suffix) {
			continue
		}
		email := strings.ToLower(m.Email)
		if allow[email] {
			continue
		}
		if delErr := DeleteOutreachMailbox(userID, m.ID); delErr != nil {
			log.Printf("prune admin mailbox %s: %v", email, delErr)
		} else {
			log.Printf("pruned admin mailbox not in ADMIN_OUTREACH_MAILBOXES: %s", email)
		}
	}
	return nil
}

func ensureAdminOutreachMailboxes(userID, domainID int64, domain string, specs []StarterMailboxSpec, allow map[string]bool) error {
	if len(specs) == 0 {
		// No explicit list: only refresh credentials for rows already on this domain (do not import new seats).
		return syncMailboxCredentialsFiltered(userID, domainID, domain, allow, false)
	}
	platform := config.InboxKitPlatform
	if platform == "" {
		platform = "GOOGLE"
	}
	mboxes := buildOrderMailboxes(domain, specs, platform)
	if err := ensureStarterMailboxRows(userID, domainID, mboxes, platform, true); err != nil {
		return fmt.Errorf("save admin mailbox rows: %w", err)
	}
	if syncErr := syncMailboxCredentialsFiltered(userID, domainID, domain, allow, false); syncErr != nil {
		log.Printf("admin mailbox sync before buy %s: %v", domain, syncErr)
	}
	pending := adminPendingMailboxLocals(userID, domainID, mboxes)
	if len(pending) == 0 {
		return nil
	}
	resp, err := buyInboxKitMailboxes(inboxkit.BuyMailboxesRequest{
		Domain:              domain,
		Mailboxes:           inboxkit.BuyItemsFromOrderMailboxes(domain, pending),
		PreferIncludedSeats: true,
	})
	if err != nil {
		return fmt.Errorf("buy admin mailboxes (included seats): %w", err)
	}
	buyOrder := strings.TrimSpace(resp.OrderID)
	if buyOrder == "" {
		buyOrder = strings.TrimSpace(resp.ID)
	}
	existing, _ := ListOutreachMailboxes(userID)
	byEmail := map[string]OutreachMailbox{}
	for _, m := range existing {
		if m.DomainID == domainID {
			byEmail[strings.ToLower(m.Email)] = m
		}
	}
	for _, bought := range resp.Mailboxes {
		email := strings.ToLower(strings.TrimSpace(bought.Email))
		if email == "" && bought.Username != "" {
			email = strings.ToLower(bought.Username) + "@" + domain
		}
		local, ok := byEmail[email]
		if !ok {
			continue
		}
		uid := strings.TrimSpace(bought.UID)
		if uid == "" {
			uid = strings.TrimSpace(bought.ID)
		}
		if uid == "" && buyOrder != "" {
			uid = pendingBuyIDPrefix + buyOrder + ":" + email
		}
		if uid != "" {
			_ = UpdateOutreachMailboxReady(local.ID, uid, local.SMTPAccountID, "provisioning")
		}
	}
	return syncMailboxCredentialsFiltered(userID, domainID, domain, allow, false)
}

func adminPendingMailboxLocals(userID, domainID int64, wanted []inboxkit.OrderMailbox) []inboxkit.OrderMailbox {
	existing, _ := ListOutreachMailboxes(userID)
	ready := map[string]bool{}
	for _, m := range existing {
		if m.DomainID != domainID {
			continue
		}
		email := strings.ToLower(m.Email)
		if m.Status == "ready" && m.SMTPAccountID > 0 {
			ready[email] = true
			continue
		}
		if m.InboxkitMailboxID != "" && !isPendingBuyMailboxID(m.InboxkitMailboxID) {
			ready[email] = true
		}
	}
	var pending []inboxkit.OrderMailbox
	for _, w := range wanted {
		email := strings.ToLower(strings.TrimSpace(w.Email))
		if email == "" || ready[email] {
			continue
		}
		pending = append(pending, w)
	}
	return pending
}

// ParseAdminOutreachMailboxSpecs parses ADMIN_OUTREACH_MAILBOXES.
// Supported entries (comma-separated):
//   - localpart
//   - localpart@domain
//   - First:Last:localpart
func ParseAdminOutreachMailboxSpecs(raw, domain string) []StarterMailboxSpec {
	raw = strings.TrimSpace(raw)
	domain = strings.ToLower(strings.TrimSpace(domain))
	if raw == "" {
		return nil
	}
	var out []StarterMailboxSpec
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Count(part, ":") >= 2 {
			bits := strings.SplitN(part, ":", 3)
			fn := strings.TrimSpace(bits[0])
			ln := strings.TrimSpace(bits[1])
			local := sanitizeLocalPart(bits[2])
			if local == "" {
				continue
			}
			if fn == "" {
				fn = titleLocal(local)
			}
			if ln == "" {
				ln = "Admin"
			}
			out = append(out, StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
			continue
		}
		local := part
		if at := strings.Index(part, "@"); at > 0 {
			local = part[:at]
			host := strings.ToLower(strings.TrimSpace(part[at+1:]))
			if domain != "" && host != "" && host != domain {
				log.Printf("admin mailbox %s skipped: domain must be %s", part, domain)
				continue
			}
		}
		local = sanitizeLocalPart(local)
		if local == "" {
			continue
		}
		out = append(out, StarterMailboxSpec{
			FirstName: titleLocal(local),
			LastName:  "Admin",
			LocalPart: local,
		})
	}
	return out
}

func titleLocal(local string) string {
	local = strings.TrimSpace(local)
	if local == "" {
		return "Admin"
	}
	seg := local
	for _, sep := range []string{".", "_", "-"} {
		if i := strings.Index(seg, sep); i > 0 {
			seg = seg[:i]
			break
		}
	}
	runes := []rune(strings.ToLower(seg))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
