package inboxkit

import (
	"encoding/json"
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

func TestSearchDomainsParsesVariants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"domain": "acme.io", "available": true, "price": 12.5},
			},
		})
	}))
	defer srv.Close()

	client := &Client{APIKey: "test", BaseURL: srv.URL, HTTP: srv.Client()}
	list, err := client.SearchDomains("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Domain != "acme.io" {
		t.Fatalf("unexpected results: %+v", list)
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
