package model

import (
	"fmt"
	"log"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/inboxkit"
)

const (
	manualBuyOrderPrefix     = "manual:buy:"
	manualConnectOrderPrefix = "manual:connect:"
)

// Optional hooks wired from routes (avoids model → notify → util → model cycle).
var (
	NotifyProvisionQueuedFn func(userID int64, kind, domain string, mailboxEmails []string)
	NotifyProvisionReadyFn  func(userID int64, domain string)
)

func isManualOrderID(id string) bool {
	id = strings.TrimSpace(id)
	return strings.HasPrefix(id, manualBuyOrderPrefix) || strings.HasPrefix(id, manualConnectOrderPrefix)
}

func IsManualPendingDomain(d OutreachDomain) bool {
	st := strings.ToLower(strings.TrimSpace(d.Status))
	return st == "pending_manual" || isManualOrderID(d.InboxkitOrderID)
}

func SpecsFromDomainMailboxes(userID, domainID int64) ([]StarterMailboxSpec, []string, error) {
	return specsFromDomainMailboxes(userID, domainID)
}

func specsFromDomainMailboxes(userID, domainID int64) ([]StarterMailboxSpec, []string, error) {
	mailboxes, err := ListOutreachMailboxes(userID)
	if err != nil {
		return nil, nil, err
	}
	var specs []StarterMailboxSpec
	var emails []string
	for _, m := range mailboxes {
		if m.DomainID != domainID {
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
		specs = append(specs, StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
		emails = append(emails, m.Email)
	}
	return specs, emails, nil
}

func fireProvisionQueued(userID int64, kind, domain string, mailboxEmails []string) {
	if NotifyProvisionQueuedFn != nil {
		NotifyProvisionQueuedFn(userID, kind, domain, mailboxEmails)
	}
}

func maybeNotifyDomainReady(userID int64, domain string, becameReady bool) {
	if !becameReady || NotifyProvisionReadyFn == nil {
		return
	}
	NotifyProvisionReadyFn(userID, domain)
}

// MarkOutreachDomainReady sets status=ready only if it was not already ready.
// Returns whether this call newly transitioned the domain to ready.
func MarkOutreachDomainReady(id int64, orderID string) (bool, error) {
	res, err := db.Exec(`
		UPDATE outreach_domains
		SET status='ready',
			inboxkit_order_id=COALESCE(NULLIF(?, ''), inboxkit_order_id),
			last_error='',
			updated_at=?
		WHERE id=? AND lower(status) <> 'ready'
	`, orderID, time.Now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListPendingManualDomains returns domains waiting for manual InboxKit fulfillment.
func ListPendingManualDomains() ([]OutreachDomain, error) {
	rows, err := db.Query(`
		SELECT id, user_id, domain, COALESCE(inboxkit_order_id,''), status, included, COALESCE(redirect_url,''),
			COALESCE(nameservers_json,''), COALESCE(last_error,''), last_synced_at, created_at, updated_at
		FROM outreach_domains
		WHERE lower(status)='pending_manual'
		   OR inboxkit_order_id LIKE 'manual:%'
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutreachDomain
	for rows.Next() {
		d, err := scanOutreachDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ListPendingManualPurchases returns mailbox/domain add-on purchases waiting for fulfillment.
func ListPendingManualPurchases() ([]MailboxPurchase, error) {
	rows, err := db.Query(`
		SELECT id FROM mailbox_purchases
		WHERE lower(status)='pending_manual'
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailboxPurchase
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := GetMailboxPurchase(id)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// queueStarterDomainOrder saves the request without calling InboxKit.
func queueStarterDomainOrder(userID int64, domain string, specs []StarterMailboxSpec, included bool) (domainID int64, orderID string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
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
		if st == "pending_manual" || isManualOrderID(existing.InboxkitOrderID) {
			_ = ensureStarterMailboxRows(userID, existing.ID, mboxes, platform, existing.Included)
			return existing.ID, existing.InboxkitOrderID, nil
		}
		if st != "error" && strings.TrimSpace(existing.InboxkitOrderID) != "" && !isPendingOrderID(existing.InboxkitOrderID) {
			_ = ensureStarterMailboxRows(userID, existing.ID, mboxes, platform, existing.Included)
			return existing.ID, existing.InboxkitOrderID, nil
		}
	}

	if included {
		if qErr := assertIncludedDomainQuota(userID); qErr != nil {
			return 0, "", qErr
		}
	}

	orderID = manualBuyOrderPrefix + fmt.Sprintf("%d-%d", userID, time.Now().UnixNano())
	domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, included)
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
	_ = UpdateOutreachDomainStatus(domainID, "pending_manual", orderID)
	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, included); mbErr != nil {
		_ = SetOutreachDomainError(domainID, "pending_manual", "Mailbox rows failed to save: "+mbErr.Error())
		return domainID, orderID, fmt.Errorf("queued but mailbox setup incomplete: %w", mbErr)
	}
	var emails []string
	for _, mb := range mboxes {
		emails = append(emails, mb.Email)
	}
	kind := "domain + mailboxes (buy)"
	if included {
		kind = "included Pro domain + mailboxes"
	}
	fireProvisionQueued(userID, kind, domain, emails)
	return domainID, orderID, nil
}

func queueConnectDomainOrder(userID int64, domain string, specs []StarterMailboxSpec) (domainID int64, orderID string, nameservers []string, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
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
		if st != "error" && oid != "" {
			_ = ensureStarterMailboxRows(userID, existing.ID, mboxes, platform, existing.Included)
			return existing.ID, oid, existing.Nameservers(), nil
		}
	}
	if qErr := assertIncludedDomainQuota(userID); qErr != nil {
		return 0, "", nil, qErr
	}

	orderID = manualConnectOrderPrefix + fmt.Sprintf("%d-%d", userID, time.Now().UnixNano())
	domainID, err = CreateOutreachDomain(userID, domain, orderID, redirect, true)
	if err != nil {
		return 0, "", nil, err
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
				return 0, "", nil, fmt.Errorf("your plan already includes one domain — buy an extra domain from Mailboxes, or continue setup for your existing domain")
			}
		}
	}
	_ = UpdateOutreachDomainStatus(domainID, "pending_manual", orderID)
	if mbErr := ensureStarterMailboxRows(userID, domainID, mboxes, platform, true); mbErr != nil {
		_ = SetOutreachDomainError(domainID, "pending_manual", "Mailbox rows failed to save: "+mbErr.Error())
		return domainID, orderID, nil, fmt.Errorf("queued but mailbox setup incomplete: %w", mbErr)
	}
	var emails []string
	for _, mb := range mboxes {
		emails = append(emails, mb.Email)
	}
	fireProvisionQueued(userID, "connect existing domain + mailboxes", domain, emails)
	return domainID, orderID, nil, nil
}

// FulfillQueuedDomainOrder runs the real InboxKit buy/connect for a pending_manual domain.
func FulfillQueuedDomainOrder(domainID int64) error {
	var userID int64
	err := db.QueryRow(`SELECT user_id FROM outreach_domains WHERE id=?`, domainID).Scan(&userID)
	if err != nil {
		return fmt.Errorf("domain not found")
	}
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return err
	}
	if !IsManualPendingDomain(d) {
		return fmt.Errorf("domain %s is not waiting for manual fulfillment (status=%s)", d.Domain, d.Status)
	}
	if !inboxkit.Configured() {
		return fmt.Errorf("%s", inboxkit.ConfiguredHint())
	}

	specs, emails, err := specsFromDomainMailboxes(userID, domainID)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return fmt.Errorf("no mailbox specs saved for domain %s", d.Domain)
	}

	wasConnect := strings.HasPrefix(d.InboxkitOrderID, manualConnectOrderPrefix)
	if err := ResetOutreachDomainForFulfill(domainID); err != nil {
		return err
	}

	prevManual := config.ManualInboxKitFulfillment
	config.ManualInboxKitFulfillment = false
	defer func() { config.ManualInboxKitFulfillment = prevManual }()

	if wasConnect {
		_, _, _, err = PlaceConnectExistingDomainOrder(userID, d.Domain, specs)
	} else {
		_, _, err = PlaceStarterDomainOrder(userID, d.Domain, specs, d.Included)
	}
	if err != nil {
		_ = UpdateOutreachDomainStatus(domainID, "pending_manual", d.InboxkitOrderID)
		_ = SetOutreachDomainError(domainID, "pending_manual", err.Error())
		return err
	}
	log.Printf("manual fulfill domain_id=%d domain=%s connect=%v mailboxes=%v", domainID, d.Domain, wasConnect, emails)
	// Paid add-on rows tied to this domain can leave the queue.
	_, _ = db.Exec(`
		UPDATE mailbox_purchases SET status='fulfilled', updated_at=?
		WHERE user_id=? AND lower(status)='pending_manual'
		  AND (domain_id=? OR payload_json ILIKE ?)
	`, time.Now(), userID, domainID, "%"+d.Domain+"%")
	return nil
}

// QueueMailboxPurchase marks a paid add-on for manual InboxKit fulfillment and notifies.
func QueueMailboxPurchase(purchaseID int64, kind, domain string, mailboxEmails []string) error {
	p, err := GetMailboxPurchase(purchaseID)
	if err != nil {
		return err
	}
	if p.Status == "fulfilled" || p.Status == "ready" {
		return nil
	}
	if err := UpdateMailboxPurchase(purchaseID, "pending_manual", "", "", ""); err != nil {
		return err
	}
	fireProvisionQueued(p.UserID, kind, domain, mailboxEmails)
	return nil
}

