package model

import (
	"encoding/json"
	"fmt"
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
		return 0, "", fmt.Errorf("InboxKit is not configured")
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

// SyncInboxKitOrder pulls order/mailbox state and stores SMTP credentials when ready.
func SyncInboxKitOrder(userID, domainID int64) error {
	d, err := GetOutreachDomain(domainID, userID)
	if err != nil {
		return err
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

	list, err := client.ListMailboxes(d.Domain)
	if err != nil {
		return err
	}
	existing, _ := ListOutreachMailboxes(userID)
	byEmail := map[string]OutreachMailbox{}
	for _, m := range existing {
		if m.DomainID == domainID || strings.HasSuffix(strings.ToLower(m.Email), "@"+strings.ToLower(d.Domain)) {
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
		local, ok := byEmail[email]
		if !ok {
			// Create row if InboxKit returned extras
			id, cErr := CreateOutreachMailbox(OutreachMailbox{
				UserID:            userID,
				DomainID:          domainID,
				InboxkitMailboxID: item.ID,
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
		if item.ID != "" && local.InboxkitMailboxID == "" {
			local.InboxkitMailboxID = item.ID
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

	if order.IsDone() {
		_ = UpdateOutreachDomainStatus(domainID, "ready", d.InboxkitOrderID)
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
			Platform:  platform,
		})
	}
	client := inboxkit.NewClient()
	resp, err := client.BuyMailboxes(inboxkit.BuyMailboxesRequest{Domain: d.Domain, Mailboxes: mboxes})
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
