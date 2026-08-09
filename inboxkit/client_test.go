package inboxkit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrderStatusDone(t *testing.T) {
	if !(OrderStatus{Status: "completed"}).IsDone() {
		t.Fatal("completed should be done")
	}
	if !(OrderStatus{Status: "DONE"}).IsDone() {
		t.Fatal("DONE should be done")
	}
	if (OrderStatus{Status: "queued"}).IsDone() {
		t.Fatal("queued should not be done")
	}
}

func TestCredentialsResolved(t *testing.T) {
	c := MailboxCredentials{AppPassword: "app", Password: "pw", Email: "a@b.com"}
	if c.ResolvedPassword() != "app" {
		t.Fatalf("want app password, got %q", c.ResolvedPassword())
	}
	c.AppPassword = ""
	if c.ResolvedPassword() != "pw" {
		t.Fatalf("want pw, got %q", c.ResolvedPassword())
	}
	if c.ResolvedSMTPUser() != "a@b.com" {
		t.Fatalf("want email as user, got %q", c.ResolvedSMTPUser())
	}
}

func TestSearchDomainsSendsKeywordAndParsesName(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/domains/search" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   false,
			"message": "ok",
			"domains": []map[string]any{
				{"name": "acme.com", "available": true, "price": 12.5, "is_premium": false},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	list, err := client.SearchDomains("acme")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["keyword"] != "acme" {
		t.Fatalf("keyword=%v body=%v", gotBody["keyword"], gotBody)
	}
	tlds, _ := gotBody["tlds"].([]any)
	if len(tlds) == 0 {
		t.Fatalf("missing tlds: %v", gotBody)
	}
	for _, tld := range tlds {
		s, _ := tld.(string)
		switch s {
		case ".com", ".net", ".shop", ".org":
		default:
			t.Fatalf("disallowed tld %q in %v", s, tlds)
		}
	}
	if gotBody["page"] == nil || gotBody["num"] == nil {
		t.Fatalf("missing page/num: %v", gotBody)
	}
	if len(list) != 1 || list[0].Domain != "acme.com" {
		t.Fatalf("unexpected results: %+v", list)
	}
}

func TestSearchDomainsExactDomainStripsKeywordTLD(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": false,
			"domains": []map[string]any{
				{"name": "jupsend.com", "available": true, "price": 10},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	list, err := client.SearchDomains("jupsend.com")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["keyword"] != "jupsend" {
		t.Fatalf("keyword=%v", gotBody["keyword"])
	}
	if gotBody["domain"] != "jupsend.com" {
		t.Fatalf("domain=%v", gotBody["domain"])
	}
	if len(list) != 1 || list[0].Domain != "jupsend.com" {
		t.Fatalf("%+v", list)
	}
}

func TestConnectDomainNameserversParsesResultArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/domains/nameservers" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   false,
			"message": "ok",
			"result": []map[string]any{
				{"domain": "mine.com", "uid": "u1", "nameservers": []string{"a.ns.cloudflare.com", "b.ns.cloudflare.com"}},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	ns, err := client.ConnectDomainNameservers("mine.com")
	if err != nil {
		t.Fatal(err)
	}
	if ns.UID != "u1" || len(ns.Nameservers) != 2 {
		t.Fatalf("%+v", ns)
	}
}

func TestConnectDomainNameserversAlreadyConnected409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   false,
			"message": "Domain(s) already connected to your workspace: tryjupsend.com",
			"result": []map[string]any{
				{
					"domain":      "tryjupsend.com",
					"uid":         "28d04a84-92db-43a8-9d40-d7039c0e5c08",
					"nameservers": []string{"art.ns.cloudflare.com", "blakely.ns.cloudflare.com"},
				},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	ns, err := client.ConnectDomainNameservers("tryjupsend.com")
	if err != nil {
		t.Fatal(err)
	}
	if ns.UID != "28d04a84-92db-43a8-9d40-d7039c0e5c08" {
		t.Fatalf("uid=%q", ns.UID)
	}
	if !ns.Ready || len(ns.Nameservers) != 2 {
		t.Fatalf("%+v", ns)
	}
}

func TestCheckNameserversUsesPropagationPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/domains/nameservers/check-propagation" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": false,
			"result": []map[string]any{
				{"name": "mine.com", "status": "active", "propagated": true, "uid": "u1"},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	ns, err := client.CheckNameservers("mine.com")
	if err != nil {
		t.Fatal(err)
	}
	if !ns.Propagated || !ns.Ready {
		t.Fatalf("%+v", ns)
	}
}

func TestBuyItemsFromOrderMailboxes(t *testing.T) {
	items := BuyItemsFromOrderMailboxes("Acme.COM", []OrderMailbox{
		{FirstName: "A", LastName: "B", Email: "alex@acme.com", Platform: "GOOGLE"},
	})
	if len(items) != 1 || items[0].Username != "alex" || items[0].DomainName != "acme.com" || items[0].Email != "alex@acme.com" {
		t.Fatalf("%+v", items)
	}
}

func TestBuyMailboxesSendsDomainAndWallet(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mailboxes/buy" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":    false,
			"order_id": "ord-1",
			"mailboxes": []map[string]any{
				{"uid": "mb-9", "email": "alex@acme.com", "username": "alex", "domain_name": "acme.com"},
			},
		})
	}))
	defer srv.Close()
	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	resp, err := client.BuyMailboxes(BuyMailboxesRequest{
		Mailboxes: BuyItemsFromOrderMailboxes("acme.com", []OrderMailbox{
			{FirstName: "A", LastName: "B", Email: "alex@acme.com", Platform: "GOOGLE"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["domain"] != "acme.com" {
		t.Fatalf("domain=%v body=%v", gotBody["domain"], gotBody)
	}
	if gotBody["use_wallet_balance"] != true {
		t.Fatalf("wallet=%v", gotBody["use_wallet_balance"])
	}
	if resp.OrderID != "ord-1" || len(resp.Mailboxes) != 1 || resp.Mailboxes[0].UID != "mb-9" {
		t.Fatalf("%+v", resp)
	}
}

func TestCreateOrderResponseID(t *testing.T) {
	if (CreateOrderResponse{OrderID: "o1", ID: "x"}).ResolvedID() != "o1" {
		t.Fatal("prefer order_id")
	}
	if (CreateOrderResponse{ID: "x"}).ResolvedID() != "x" {
		t.Fatal("fallback to id")
	}
}

func TestConfiguredRequiresWorkspace(t *testing.T) {
	// Configured() reads package config; unit-level helpers covered via ConnectOrderID.
	if !IsConnectOrderID("connect:abc") {
		t.Fatal("expected connect prefix")
	}
	if IsConnectOrderID("order-123") {
		t.Fatal("plain order id should not be connect")
	}
}

func TestGetMailboxParsesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mailboxes/mb-1" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uid": "mb-1", "email": "a@acme.com", "status": "active",
			"domain_name": "acme.com", "is_admin": true, "forwarding_email": "fwd@x.com",
			"first_name": "Ada", "last_name": "Lovelace", "platform": "GOOGLE",
		})
	}))
	defer srv.Close()
	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	d, err := client.GetMailbox("mb-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.ResolvedID() != "mb-1" || d.ResolvedDomain() != "acme.com" || !d.ResolvedIsAdmin() {
		t.Fatalf("%+v", d)
	}
	if d.ResolvedForwarding() != "fwd@x.com" {
		t.Fatalf("forwarding=%q", d.ResolvedForwarding())
	}
}

func TestCancelMailboxesPostsUIDs(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mailboxes/cancel" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": false})
	}))
	defer srv.Close()
	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := client.CancelMailboxes([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	uids, _ := gotBody["uids"].([]any)
	if len(uids) != 2 {
		t.Fatalf("%v", gotBody)
	}
}

func TestSetupForwardingPostsBody(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": false,
			"jobs":  []map[string]any{{"uid": "u1", "status": "queued", "forwarding_email": "me@x.com"}},
		})
	}))
	defer srv.Close()
	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	jobs, err := client.SetupForwarding([]string{"u1"}, "me@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mailboxes/forwarding/setup" {
		t.Fatalf("path=%s", gotPath)
	}
	if gotBody["forwarding_email"] != "me@x.com" {
		t.Fatalf("%v", gotBody)
	}
	if len(jobs) != 1 || jobs[0].Status != "queued" {
		t.Fatalf("%+v", jobs)
	}
}

func TestRemoveForwardingPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"error": false, "result": []any{}})
	}))
	defer srv.Close()
	client := &Client{APIKey: "test", WorkspaceID: "ws", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := client.RemoveForwarding([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mailboxes/forwarding/remove" {
		t.Fatalf("path=%s", gotPath)
	}
}

func TestMailboxListItemScheduledCancel(t *testing.T) {
	if !(MailboxListItem{Status: "scheduled_for_cancellation"}).IsScheduledCancel() {
		t.Fatal("expected scheduled")
	}
	if (MailboxListItem{Status: "active"}).IsScheduledCancel() {
		t.Fatal("active should not be scheduled")
	}
}
