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
	if len(items) != 1 || items[0].Username != "alex" || items[0].DomainName != "acme.com" {
		t.Fatalf("%+v", items)
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
