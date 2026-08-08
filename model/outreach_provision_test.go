package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/inboxkit"
)

func TestCountActiveIncludedDomainsQuota(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("quota-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	left, err := IncludedDomainQuotaRemaining(userID)
	if err != nil || left != 1 {
		t.Fatalf("left=%d err=%v", left, err)
	}
	if _, err := CreateOutreachDomain(userID, "one.example", "ord-1", "http://localhost", true); err != nil {
		t.Fatal(err)
	}
	n, err := CountActiveIncludedDomains(userID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	left, err = IncludedDomainQuotaRemaining(userID)
	if err != nil || left != 0 {
		t.Fatalf("left after claim=%d err=%v", left, err)
	}
	if err := assertIncludedDomainQuota(userID); err == nil {
		t.Fatal("expected quota error")
	}
}

func TestPlaceStarterDomainOrderIdempotentAndQuota(t *testing.T) {
	db.OpenTestDB(t)
	prevKey, prevWS := config.InboxKitAPIKey, config.InboxKitWorkspaceID
	prevEmail := config.InboxKitRegistrantEmail
	prevManual := config.ManualInboxKitFulfillment
	t.Cleanup(func() {
		config.InboxKitAPIKey = prevKey
		config.InboxKitWorkspaceID = prevWS
		config.InboxKitRegistrantEmail = prevEmail
		config.ManualInboxKitFulfillment = prevManual
		createInboxKitOrder = func(req inboxkit.CreateOrderRequest) (inboxkit.CreateOrderResponse, error) {
			return inboxkit.NewClient().CreateOrder(req)
		}
	})
	config.ManualInboxKitFulfillment = false
	config.InboxKitAPIKey = "test-key"
	config.InboxKitWorkspaceID = "test-ws"
	config.InboxKitRegistrantEmail = "reg@example.com"

	calls := 0
	createInboxKitOrder = func(req inboxkit.CreateOrderRequest) (inboxkit.CreateOrderResponse, error) {
		calls++
		return inboxkit.CreateOrderResponse{OrderID: "order-abc", Status: "processing"}, nil
	}

	userID, err := CreateUser(fmt.Sprintf("order-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	specs := []StarterMailboxSpec{{FirstName: "Ada", LastName: "Lovelace", LocalPart: "ada"}}

	id1, oid1, err := PlaceStarterDomainOrder(userID, "acme-test.com", specs, true)
	if err != nil {
		t.Fatal(err)
	}
	if oid1 != "order-abc" || id1 == 0 {
		t.Fatalf("id=%d oid=%q", id1, oid1)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}

	id2, oid2, err := PlaceStarterDomainOrder(userID, "acme-test.com", specs, true)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id1 || oid2 != oid1 {
		t.Fatalf("idempotent mismatch id %d/%d oid %q/%q", id1, id2, oid1, oid2)
	}
	if calls != 1 {
		t.Fatalf("second call should not CreateOrder, calls=%d", calls)
	}

	_, _, err = PlaceStarterDomainOrder(userID, "other-test.com", specs, true)
	if err == nil || !strings.Contains(err.Error(), "already includes one domain") {
		t.Fatalf("expected quota error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("quota path must not CreateOrder, calls=%d", calls)
	}

	// Paid add-on path (included=false) is allowed.
	createInboxKitOrder = func(req inboxkit.CreateOrderRequest) (inboxkit.CreateOrderResponse, error) {
		calls++
		return inboxkit.CreateOrderResponse{OrderID: "order-paid", Status: "processing"}, nil
	}
	_, oidPaid, err := PlaceStarterDomainOrder(userID, "paid-addon.com", specs, false)
	if err != nil {
		t.Fatal(err)
	}
	if oidPaid != "order-paid" {
		t.Fatalf("oid=%q", oidPaid)
	}
}

func TestBuyPendingMailboxesDoesNotDoubleCharge(t *testing.T) {
	db.OpenTestDB(t)
	prevBuy := buyInboxKitMailboxes
	t.Cleanup(func() { buyInboxKitMailboxes = prevBuy })

	buys := 0
	buyInboxKitMailboxes = func(req inboxkit.BuyMailboxesRequest) (inboxkit.BuyMailboxesResponse, error) {
		buys++
		return inboxkit.BuyMailboxesResponse{
			OrderID: "buy-1",
			Mailboxes: []struct {
				UID        string `json:"uid"`
				ID         string `json:"id"`
				Email      string `json:"email"`
				DomainName string `json:"domain_name"`
				Username   string `json:"username"`
				Status     string `json:"status"`
			}{
				// Empty UID simulates InboxKit not returning ids — we still must placeholder.
				{Username: "ada", DomainName: "connect.example", Status: "pending"},
			},
		}, nil
	}

	userID, err := CreateUser(fmt.Sprintf("buy-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := CreateOutreachDomain(userID, "connect.example", "connect:uid", "http://localhost", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpsertOutreachMailbox(OutreachMailbox{
		UserID: userID, DomainID: domainID, Email: "ada@connect.example",
		FirstName: "Ada", LastName: "L", Platform: "GOOGLE", Status: "provisioning", Included: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := buyPendingMailboxesForDomain(userID, domainID, "connect.example", "GOOGLE"); err != nil {
		t.Fatal(err)
	}
	if buys != 1 {
		t.Fatalf("buys=%d", buys)
	}
	mailboxes, _ := ListOutreachMailboxes(userID)
	if len(mailboxes) != 1 || mailboxes[0].InboxkitMailboxID == "" {
		t.Fatalf("expected placeholder id, got %+v", mailboxes)
	}
	if err := buyPendingMailboxesForDomain(userID, domainID, "connect.example", "GOOGLE"); err != nil {
		t.Fatal(err)
	}
	if buys != 1 {
		t.Fatalf("second buy should be skipped, buys=%d", buys)
	}
}

func TestPlaceStarterDomainOrderRequiresRegistrant(t *testing.T) {
	db.OpenTestDB(t)
	prevKey, prevWS := config.InboxKitAPIKey, config.InboxKitWorkspaceID
	prevEmail, prevName, prevOrg := config.InboxKitRegistrantEmail, config.InboxKitRegistrantName, config.InboxKitRegistrantOrg
	prevManual := config.ManualInboxKitFulfillment
	t.Cleanup(func() {
		config.InboxKitAPIKey = prevKey
		config.InboxKitWorkspaceID = prevWS
		config.InboxKitRegistrantEmail = prevEmail
		config.InboxKitRegistrantName = prevName
		config.InboxKitRegistrantOrg = prevOrg
		config.ManualInboxKitFulfillment = prevManual
	})
	config.ManualInboxKitFulfillment = false
	config.InboxKitAPIKey = "k"
	config.InboxKitWorkspaceID = "w"
	config.InboxKitRegistrantEmail = ""
	config.InboxKitRegistrantName = ""
	config.InboxKitRegistrantOrg = ""

	userID, err := CreateUser(fmt.Sprintf("reg-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = PlaceStarterDomainOrder(userID, "noreg.com", []StarterMailboxSpec{{FirstName: "A", LastName: "B", LocalPart: "a"}}, true)
	if err == nil || !strings.Contains(err.Error(), "registration contact") {
		t.Fatalf("expected registrant error, got %v", err)
	}
}
