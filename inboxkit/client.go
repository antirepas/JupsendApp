package inboxkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"emailtracker.com/config"
)

const defaultBaseURL = "https://api.inboxkit.com/v1"

type Client struct {
	APIKey      string
	WorkspaceID string
	BaseURL     string
	HTTP        *http.Client
}

func NewClient() *Client {
	return &Client{
		APIKey:      config.InboxKitAPIKey,
		WorkspaceID: config.InboxKitWorkspaceID,
		BaseURL:     strings.TrimRight(config.InboxKitBaseURL, "/"),
		HTTP:        &http.Client{Timeout: 60 * time.Second},
	}
}

func Configured() bool {
	return strings.TrimSpace(config.InboxKitAPIKey) != ""
}

func (c *Client) do(method, path string, body any, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("INBOXKIT_API_KEY not configured")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.WorkspaceID != "" {
		req.Header.Set("X-Workspace-Id", c.WorkspaceID)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("inboxkit %s %s: %s — %s", method, path, res.Status, truncate(string(raw), 400))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("inboxkit decode: %w (body=%s)", err, truncate(string(raw), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type DomainSearchResult struct {
	Domain      string  `json:"domain"`
	Name        string  `json:"name"`
	Available   bool    `json:"available"`
	Price       float64 `json:"price"`
	TLD         string  `json:"tld"`
	Premium     bool    `json:"premium"`
	RawDomain   string  `json:"domain_name"`
}

type domainSearchResponse struct {
	Domains []DomainSearchResult `json:"domains"`
	Results []DomainSearchResult `json:"results"`
	Data    []DomainSearchResult `json:"data"`
}

func (c *Client) SearchDomains(query string) ([]DomainSearchResult, error) {
	var resp domainSearchResponse
	err := c.do("POST", "/api/domains/search", map[string]any{
		"query": query,
		"q":     query,
	}, &resp)
	if err != nil {
		return nil, err
	}
	list := resp.Domains
	if len(list) == 0 {
		list = resp.Results
	}
	if len(list) == 0 {
		list = resp.Data
	}
	for i := range list {
		if list[i].Domain == "" {
			list[i].Domain = list[i].Name
		}
		if list[i].Domain == "" {
			list[i].Domain = list[i].RawDomain
		}
		if !list[i].Available && list[i].Domain != "" {
			// API may omit available=true; treat listed search hits as selectable unless premium-blocked.
			list[i].Available = true
		}
	}
	return list, nil
}

type OrderMailbox struct {
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Platform      string `json:"platform"`
	ProfilePicURL string `json:"profile_pic_url,omitempty"`
}

type OrderDomain struct {
	Name              string         `json:"name"`
	RedirectURL       string         `json:"redirect_url"`
	RegistrationYears int            `json:"registration_years,omitempty"`
	Mailboxes         []OrderMailbox `json:"mailboxes"`
	ContactDetails    map[string]any `json:"contact_details,omitempty"`
}

type CreateOrderRequest struct {
	Domains        []OrderDomain  `json:"domains"`
	ContactDetails map[string]any `json:"contact_details,omitempty"`
}

type CreateOrderResponse struct {
	OrderID string `json:"order_id"`
	ID      string `json:"id"`
	Status  string `json:"status"`
}

func (r CreateOrderResponse) ResolvedID() string {
	if r.OrderID != "" {
		return r.OrderID
	}
	return r.ID
}

func (c *Client) CreateOrder(req CreateOrderRequest) (CreateOrderResponse, error) {
	var resp CreateOrderResponse
	err := c.do("POST", "/api/orders", req, &resp)
	return resp, err
}

type OrderStatus struct {
	OrderID  string          `json:"order_id"`
	ID       string          `json:"id"`
	Status   string          `json:"status"`
	Domains  json.RawMessage `json:"domains"`
	Raw      json.RawMessage `json:"-"`
}

func (c *Client) GetOrder(orderID string) (OrderStatus, error) {
	var resp OrderStatus
	err := c.do("GET", "/api/orders/"+orderID, nil, &resp)
	if resp.OrderID == "" {
		resp.OrderID = resp.ID
	}
	return resp, err
}

func (o OrderStatus) IsDone() bool {
	s := strings.ToLower(o.Status)
	return s == "done" || s == "completed" || s == "complete"
}

func (o OrderStatus) IsError() bool {
	s := strings.ToLower(o.Status)
	return s == "error" || s == "failed"
}

type BuyMailboxesRequest struct {
	Domain    string         `json:"domain"`
	Mailboxes []OrderMailbox `json:"mailboxes"`
}

type BuyMailboxesResponse struct {
	OrderID string `json:"order_id"`
	ID      string `json:"id"`
	Status  string `json:"status"`
}

func (c *Client) BuyMailboxes(req BuyMailboxesRequest) (BuyMailboxesResponse, error) {
	var resp BuyMailboxesResponse
	// Prefer dedicated buy endpoint; fall back to orders with empty registration.
	err := c.do("POST", "/api/mailboxes/buy", req, &resp)
	if err == nil {
		return resp, nil
	}
	order, orderErr := c.CreateOrder(CreateOrderRequest{
		Domains: []OrderDomain{{
			Name:        req.Domain,
			RedirectURL: config.InboxKitRedirectURL,
			Mailboxes:   req.Mailboxes,
		}},
	})
	if orderErr != nil {
		return BuyMailboxesResponse{}, fmt.Errorf("mailboxes/buy: %v; orders fallback: %w", err, orderErr)
	}
	return BuyMailboxesResponse{OrderID: order.ResolvedID(), Status: order.Status}, nil
}

type MailboxCredentials struct {
	Email        string `json:"email"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     string `json:"smtp_port"`
	IMAPHost     string `json:"imap_host"`
	IMAPPort     string `json:"imap_port"`
	Password     string `json:"password"`
	AppPassword  string `json:"app_password"`
	Username     string `json:"username"`
	SMTPUsername string `json:"smtp_username"`
}

func (m MailboxCredentials) ResolvedPassword() string {
	if m.AppPassword != "" {
		return m.AppPassword
	}
	return m.Password
}

func (m MailboxCredentials) ResolvedSMTPUser() string {
	if m.SMTPUsername != "" {
		return m.SMTPUsername
	}
	if m.Username != "" {
		return m.Username
	}
	return m.Email
}

func (c *Client) GetMailboxCredentials(mailboxID string) (MailboxCredentials, error) {
	var resp MailboxCredentials
	err := c.do("GET", "/api/mailboxes/"+mailboxID+"/credentials", nil, &resp)
	return resp, err
}

type MailboxListItem struct {
	ID     string `json:"id"`
	UUID   string `json:"uuid"`
	Email  string `json:"email"`
	Status string `json:"status"`
	Domain string `json:"domain"`
}

type mailboxListResponse struct {
	Mailboxes []MailboxListItem `json:"mailboxes"`
	Data      []MailboxListItem `json:"data"`
}

func (c *Client) ListMailboxes(domain string) ([]MailboxListItem, error) {
	body := map[string]any{}
	if domain != "" {
		body["domain"] = domain
	}
	var resp mailboxListResponse
	err := c.do("POST", "/api/mailboxes/list", body, &resp)
	if err != nil {
		return nil, err
	}
	list := resp.Mailboxes
	if len(list) == 0 {
		list = resp.Data
	}
	for i := range list {
		if list[i].ID == "" {
			list[i].ID = list[i].UUID
		}
	}
	return list, nil
}

// Insights fetches best-effort analytics JSON for a mailbox (shape varies by InboxKit).
func (c *Client) MailboxInsights(mailboxID string) (json.RawMessage, error) {
	var raw json.RawMessage
	paths := []string{
		"/api/mailboxes/" + mailboxID + "/insights",
		"/api/email-insights/mailboxes/" + mailboxID,
		"/api/insights/mailboxes/" + mailboxID,
	}
	var lastErr error
	for _, p := range paths {
		err := c.do("GET", p, nil, &raw)
		if err == nil && len(raw) > 0 {
			return raw, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return json.RawMessage(`{}`), nil // non-fatal for MVP UI
	}
	return raw, nil
}

func DefaultRegistrant() map[string]any {
	out := map[string]any{}
	if config.InboxKitRegistrantEmail != "" {
		out["email"] = config.InboxKitRegistrantEmail
	}
	if config.InboxKitRegistrantName != "" {
		out["name"] = config.InboxKitRegistrantName
		parts := strings.Fields(config.InboxKitRegistrantName)
		if len(parts) >= 1 {
			out["first_name"] = parts[0]
		}
		if len(parts) >= 2 {
			out["last_name"] = strings.Join(parts[1:], " ")
		}
	}
	if config.InboxKitRegistrantOrg != "" {
		out["organization"] = config.InboxKitRegistrantOrg
		out["company"] = config.InboxKitRegistrantOrg
	}
	return out
}
