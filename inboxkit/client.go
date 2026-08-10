package inboxkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	base := strings.TrimRight(config.InboxKitBaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{
		APIKey:      config.InboxKitAPIKey,
		WorkspaceID: config.InboxKitWorkspaceID,
		BaseURL:     base,
		HTTP:        &http.Client{Timeout: 60 * time.Second},
	}
}

func Configured() bool {
	return strings.TrimSpace(config.InboxKitAPIKey) != "" &&
		strings.TrimSpace(config.InboxKitWorkspaceID) != ""
}

func ConfiguredHint() string {
	missing := []string{}
	if strings.TrimSpace(config.InboxKitAPIKey) == "" {
		missing = append(missing, "INBOXKIT_API_KEY")
	}
	if strings.TrimSpace(config.InboxKitWorkspaceID) == "" {
		missing = append(missing, "INBOXKIT_WORKSPACE_ID")
	}
	if len(missing) == 0 {
		return ""
	}
	return "InboxKit is not configured — set " + strings.Join(missing, " and ")
}

func (c *Client) do(method, path string, body any, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("INBOXKIT_API_KEY not configured")
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("INBOXKIT_WORKSPACE_ID not configured")
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
	req.Header.Set("X-Workspace-Id", c.WorkspaceID)
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

// doAllowStatuses is like do, but treats listed HTTP statuses as success and still unmarshals the body.
func (c *Client) doAllowStatuses(method, path string, body any, out any, allow ...int) (status int, err error) {
	if c.APIKey == "" {
		return 0, fmt.Errorf("INBOXKIT_API_KEY not configured")
	}
	if c.WorkspaceID == "" {
		return 0, fmt.Errorf("INBOXKIT_WORKSPACE_ID not configured")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Workspace-Id", c.WorkspaceID)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	status = res.StatusCode
	allowed := status < 400
	for _, a := range allow {
		if status == a {
			allowed = true
			break
		}
	}
	if !allowed {
		return status, fmt.Errorf("inboxkit %s %s: %s — %s", method, path, res.Status, truncate(string(raw), 400))
	}
	if out == nil || len(raw) == 0 {
		return status, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return status, fmt.Errorf("inboxkit decode: %w (body=%s)", err, truncate(string(raw), 200))
	}
	return status, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type DomainSearchResult struct {
	Domain    string  `json:"domain"`
	Name      string  `json:"name"`
	Available bool    `json:"available"`
	Price     float64 `json:"price"`
	TLD       string  `json:"tld"`
	Premium   bool    `json:"premium"`
	IsPremium bool    `json:"is_premium"`
	Banned    bool    `json:"banned"`
	RawDomain string  `json:"domain_name"`
}

func (d DomainSearchResult) ResolvedDomain() string {
	if d.Domain != "" {
		return d.Domain
	}
	if d.Name != "" {
		return d.Name
	}
	return d.RawDomain
}

type domainSearchResponse struct {
	Error   bool                 `json:"error"`
	Message string               `json:"message"`
	Domains []DomainSearchResult `json:"domains"`
	Results []DomainSearchResult `json:"results"`
	Data    []DomainSearchResult `json:"data"`
}

func (c *Client) SearchDomains(query string) ([]DomainSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("enter a keyword to search")
	}
	keyword := query
	body := map[string]any{
		"page":                                1,
		"num":                                 20,
		"show_unavailable":                    false,
		"check_banned":                        true,
		"check_google_workspace_availability": true,
		"check_ms365_workspace_availability":  false,
		// InboxKit only allows these TLDs on /api/domains/search.
		"tlds": []string{".com", ".net", ".shop", ".org"},
	}
	// Exact domain lookups: send domain + keyword without TLD.
	if strings.Contains(query, ".") {
		body["domain"] = strings.ToLower(query)
		if i := strings.Index(query, "."); i > 0 {
			keyword = query[:i]
		}
	}
	body["keyword"] = keyword

	var resp domainSearchResponse
	err := c.do("POST", "/api/domains/search", body, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "domain search failed"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	list := resp.Domains
	if len(list) == 0 {
		list = resp.Results
	}
	if len(list) == 0 {
		list = resp.Data
	}
	out := make([]DomainSearchResult, 0, len(list))
	for _, item := range list {
		item.Domain = item.ResolvedDomain()
		if item.Domain == "" || item.Banned {
			continue
		}
		if item.IsPremium {
			item.Premium = true
		}
		// API may omit available=true for listed hits.
		if !item.Available {
			item.Available = true
		}
		out = append(out, item)
	}
	return out, nil
}

type OrderMailbox struct {
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Username      string `json:"username,omitempty"`
	Platform      string `json:"platform"`
	ProfilePicURL string `json:"profile_pic_url,omitempty"`
}

func (m OrderMailbox) ResolvedUsername() string {
	if m.Username != "" {
		return strings.ToLower(strings.TrimSpace(m.Username))
	}
	email := strings.ToLower(strings.TrimSpace(m.Email))
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
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
	OrderID string          `json:"order_id"`
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Domains json.RawMessage `json:"domains"`
	Raw     json.RawMessage `json:"-"`
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

// BuyMailboxItem matches POST /api/mailboxes/buy.
// InboxKit rejects unknown fields (e.g. "email"); use username + domain_name only.
type BuyMailboxItem struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Username       string `json:"username"`
	Platform       string `json:"platform"`
	DomainName     string `json:"domain_name"`
	ProfilePicture string `json:"profile_picture,omitempty"`
}

type BuyMailboxesRequest struct {
	// Domain is derived locally only — not sent to InboxKit (each item has domain_name).
	Domain           string           `json:"-"`
	Mailboxes        []BuyMailboxItem `json:"mailboxes"`
	UseWalletBalance bool `json:"use_wallet_balance"`
	// PreferIncludedSeats skips charging the InboxKit wallet (use plan-included seats).
	PreferIncludedSeats bool `json:"-"`
}

type BuyMailboxesResponse struct {
	Error     bool   `json:"error"`
	Message   string `json:"message"`
	OrderID   string `json:"order_id"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	Mailboxes []struct {
		UID        string `json:"uid"`
		ID         string `json:"id"`
		Email      string `json:"email"`
		DomainName string `json:"domain_name"`
		Username   string `json:"username"`
		Status     string `json:"status"`
	} `json:"mailboxes"`
}

func BuyItemsFromOrderMailboxes(domain string, mailboxes []OrderMailbox) []BuyMailboxItem {
	domain = strings.ToLower(strings.TrimSpace(domain))
	out := make([]BuyMailboxItem, 0, len(mailboxes))
	for _, m := range mailboxes {
		user := m.ResolvedUsername()
		if user == "" {
			continue
		}
		platform := m.Platform
		if platform == "" {
			platform = "GOOGLE"
		}
		itemDomain := domain
		if itemDomain == "" && strings.Contains(m.Email, "@") {
			itemDomain = strings.ToLower(m.Email[strings.Index(m.Email, "@")+1:])
		}
		out = append(out, BuyMailboxItem{
			FirstName:  m.FirstName,
			LastName:   m.LastName,
			Username:   user,
			Platform:   platform,
			DomainName: itemDomain,
		})
	}
	return out
}

func (c *Client) BuyMailboxes(req BuyMailboxesRequest) (BuyMailboxesResponse, error) {
	if strings.TrimSpace(req.Domain) == "" && len(req.Mailboxes) > 0 {
		req.Domain = strings.ToLower(strings.TrimSpace(req.Mailboxes[0].DomainName))
	}
	for i := range req.Mailboxes {
		if req.Mailboxes[i].DomainName == "" && req.Domain != "" {
			req.Mailboxes[i].DomainName = req.Domain
		}
		req.Mailboxes[i].Username = strings.ToLower(strings.TrimSpace(req.Mailboxes[i].Username))
		req.Mailboxes[i].DomainName = strings.ToLower(strings.TrimSpace(req.Mailboxes[i].DomainName))
	}
	// Charge wallet by default; admin/included-seat buys set PreferIncludedSeats.
	if req.PreferIncludedSeats {
		req.UseWalletBalance = false
	} else {
		req.UseWalletBalance = true
	}
	if len(req.Mailboxes) == 0 {
		return BuyMailboxesResponse{}, fmt.Errorf("at least one mailbox is required")
	}
	var resp BuyMailboxesResponse
	err := c.do("POST", "/api/mailboxes/buy", req, &resp)
	if err != nil {
		return resp, err
	}
	if resp.Error {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "mailbox purchase failed"
		}
		return resp, fmt.Errorf("%s", msg)
	}
	return resp, nil
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
	mailboxID = strings.TrimSpace(mailboxID)
	if mailboxID == "" {
		return MailboxCredentials{}, fmt.Errorf("mailbox id required")
	}
	var resp MailboxCredentials
	// Current InboxKit API: GET /api/mailboxes/show-credentials?uid=...
	err := c.do("GET", "/api/mailboxes/show-credentials?uid="+url.QueryEscape(mailboxID), nil, &resp)
	if err == nil {
		return resp, nil
	}
	// Legacy path fallback (some docs still mention /mailboxes/:id/credentials).
	var legacy MailboxCredentials
	if err2 := c.do("GET", "/api/mailboxes/"+url.PathEscape(mailboxID)+"/credentials", nil, &legacy); err2 == nil {
		return legacy, nil
	}
	return resp, err
}

// GetMailboxCredentialsByEmail fetches SMTP/IMAP secrets by email address.
func (c *Client) GetMailboxCredentialsByEmail(email string) (MailboxCredentials, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return MailboxCredentials{}, fmt.Errorf("email required")
	}
	var resp MailboxCredentials
	err := c.do("GET", "/api/mailboxes/show-credentials?email="+url.QueryEscape(email), nil, &resp)
	return resp, err
}

type MailboxListItem struct {
	ID               string `json:"id"`
	UID              string `json:"uid"`
	UUID             string `json:"uuid"`
	MongoID          string `json:"_id"`
	Email            string `json:"email"`
	Username         string `json:"username"`
	Status           string `json:"status"`
	Domain           string `json:"domain"`
	DomainName       string `json:"domain_name"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	Platform         string `json:"platform"`
	Role             string `json:"role"`
	IsAdmin          bool   `json:"is_admin"`
	Admin            bool   `json:"admin"`
	ForwardingEmail  string `json:"forwarding_email"`
	Forwarding       string `json:"forwarding"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	CancelledAt      string `json:"cancelled_at"`
	ScheduledCancel  bool   `json:"scheduled_for_cancellation"`
	CancelScheduled  bool   `json:"cancel_scheduled"`
}

func (m MailboxListItem) ResolvedID() string {
	if m.ID != "" {
		return m.ID
	}
	if m.UID != "" {
		return m.UID
	}
	if m.UUID != "" {
		return m.UUID
	}
	return m.MongoID
}

func (m MailboxListItem) ResolvedDomain() string {
	if m.DomainName != "" {
		return m.DomainName
	}
	return m.Domain
}

// ResolvedEmail prefers explicit email; otherwise username@domain_name (list API often omits email).
func (m MailboxListItem) ResolvedEmail() string {
	if e := strings.ToLower(strings.TrimSpace(m.Email)); e != "" {
		return e
	}
	user := strings.ToLower(strings.TrimSpace(m.Username))
	host := strings.ToLower(strings.TrimSpace(m.ResolvedDomain()))
	if user == "" || host == "" {
		return ""
	}
	return user + "@" + host
}

func (m MailboxListItem) ResolvedForwarding() string {
	if m.ForwardingEmail != "" {
		return m.ForwardingEmail
	}
	return m.Forwarding
}

func (m MailboxListItem) ResolvedIsAdmin() bool {
	if m.IsAdmin || m.Admin {
		return true
	}
	return strings.EqualFold(m.Role, "admin")
}

func (m MailboxListItem) IsScheduledCancel() bool {
	if m.ScheduledCancel || m.CancelScheduled {
		return true
	}
	s := strings.ToLower(m.Status)
	return s == "scheduled_for_cancellation" || s == "scheduled_cancel" || s == "cancelling" || s == "cancel_scheduled"
}

type mailboxListResponse struct {
	Mailboxes []MailboxListItem `json:"mailboxes"`
	Data      []MailboxListItem `json:"data"`
}

func (c *Client) ListMailboxes(domain string) ([]MailboxListItem, error) {
	body := map[string]any{
		"page":  1,
		"limit": 100,
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain != "" {
		// InboxKit rejects unknown fields (e.g. domain_name) on this endpoint.
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
			list[i].ID = list[i].ResolvedID()
		}
	}
	if domain == "" {
		return list, nil
	}
	// Some workspaces ignore the domain filter — keep only matching seats.
	filtered := make([]MailboxListItem, 0, len(list))
	suffix := "@" + domain
	for _, item := range list {
		email := item.ResolvedEmail()
		host := strings.ToLower(strings.TrimSpace(item.ResolvedDomain()))
		if host == domain || strings.HasSuffix(email, suffix) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type NameserverResult struct {
	Domain      string   `json:"domain"`
	UID         string   `json:"uid"`
	Nameservers []string `json:"nameservers"`
	Propagated  bool     `json:"propagated"`
	Ready       bool     `json:"ready"`
	Status      string   `json:"status"`
}

type nameserverCreateResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Result  []struct {
		Domain      string   `json:"domain"`
		Nameservers []string `json:"nameservers"`
		UID         string   `json:"uid"`
	} `json:"result"`
}

// ConnectDomainNameservers registers an existing domain for connection and returns InboxKit nameservers.
// HTTP 409 (already connected to this workspace) is treated as success when the body includes nameservers/uid.
func (c *Client) ConnectDomainNameservers(domain string) (NameserverResult, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var resp nameserverCreateResponse
	status, err := c.doAllowStatuses("POST", "/api/domains/nameservers", map[string]any{
		"domains": []string{domain},
	}, &resp, http.StatusConflict)
	if err != nil {
		return NameserverResult{}, err
	}
	if resp.Error && status != http.StatusConflict {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "failed to create domain nameservers"
		}
		return NameserverResult{}, fmt.Errorf("%s", msg)
	}
	for _, item := range resp.Result {
		name := strings.ToLower(strings.TrimSpace(item.Domain))
		if name == "" || name == domain {
			return NameserverResult{
				Domain:      domain,
				UID:         item.UID,
				Nameservers: item.Nameservers,
				Ready:       status == http.StatusConflict,
				Propagated:  status == http.StatusConflict,
			}, nil
		}
	}
	if len(resp.Result) > 0 {
		item := resp.Result[0]
		return NameserverResult{
			Domain:      domain,
			UID:         item.UID,
			Nameservers: item.Nameservers,
			Ready:       status == http.StatusConflict,
			Propagated:  status == http.StatusConflict,
		}, nil
	}
	if status == http.StatusConflict {
		return NameserverResult{}, fmt.Errorf("domain already connected but InboxKit returned no nameserver details")
	}
	return NameserverResult{}, fmt.Errorf("no nameservers returned for %s", domain)
}

// GetNameservers is an alias for ConnectDomainNameservers (creates/returns NS for connection).
func (c *Client) GetNameservers(domain string) (NameserverResult, error) {
	return c.ConnectDomainNameservers(domain)
}

type nameserverCheckResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Result  []struct {
		UID        string `json:"uid"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Propagated bool   `json:"propagated"`
	} `json:"result"`
}

// CheckNameservers reports whether registrar NS have propagated to InboxKit.
func (c *Client) CheckNameservers(domain string) (NameserverResult, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var resp nameserverCheckResponse
	err := c.do("POST", "/api/domains/nameservers/check-propagation", map[string]any{
		"domains": []string{domain},
	}, &resp)
	if err != nil {
		return NameserverResult{}, err
	}
	if resp.Error {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "nameserver check failed"
		}
		return NameserverResult{}, fmt.Errorf("%s", msg)
	}
	for _, item := range resp.Result {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name != "" && name != domain {
			continue
		}
		active := item.Propagated || strings.EqualFold(item.Status, "active")
		return NameserverResult{
			Domain:     domain,
			UID:        item.UID,
			Propagated: active,
			Ready:      active,
			Status:     item.Status,
		}, nil
	}
	if len(resp.Result) > 0 {
		item := resp.Result[0]
		active := item.Propagated || strings.EqualFold(item.Status, "active")
		return NameserverResult{
			Domain:     domain,
			UID:        item.UID,
			Propagated: active,
			Ready:      active,
			Status:     item.Status,
		}, nil
	}
	return NameserverResult{Domain: domain}, nil
}

type apiErrorBody struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

func (r apiErrorBody) errOrNil(fallback string) error {
	if !r.Error {
		return nil
	}
	msg := strings.TrimSpace(r.Message)
	if msg == "" {
		msg = fallback
	}
	return fmt.Errorf("%s", msg)
}

// MailboxDetail is GET /api/mailboxes/:id (flexible field names).
type MailboxDetail struct {
	MailboxListItem
	Username string `json:"username"`
	Message  string `json:"message"`
	Error    bool   `json:"error"`
}

func (c *Client) GetMailbox(mailboxID string) (MailboxDetail, error) {
	mailboxID = strings.TrimSpace(mailboxID)
	if mailboxID == "" {
		return MailboxDetail{}, fmt.Errorf("mailbox id required")
	}
	var resp MailboxDetail
	err := c.do("GET", "/api/mailboxes/"+mailboxID, nil, &resp)
	if err != nil {
		return resp, err
	}
	if err := (apiErrorBody{Error: resp.Error, Message: resp.Message}).errOrNil("failed to get mailbox"); err != nil {
		return resp, err
	}
	if resp.ID == "" {
		resp.ID = resp.ResolvedID()
	}
	if resp.Domain == "" {
		resp.Domain = resp.ResolvedDomain()
	}
	return resp, nil
}

type UpdateMailboxRequest struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

func (c *Client) UpdateMailbox(mailboxID string, req UpdateMailboxRequest) error {
	mailboxID = strings.TrimSpace(mailboxID)
	if mailboxID == "" {
		return fmt.Errorf("mailbox id required")
	}
	var resp apiErrorBody
	err := c.do("POST", "/api/mailboxes/"+mailboxID+"/update", req, &resp)
	if err != nil {
		return err
	}
	return resp.errOrNil("failed to update mailbox")
}

type CancelMailboxesRequest struct {
	UIDs []string `json:"uids"`
}

func (c *Client) CancelMailboxes(uids []string) error {
	cleaned := make([]string, 0, len(uids))
	for _, u := range uids {
		u = strings.TrimSpace(u)
		if u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) == 0 {
		return fmt.Errorf("at least one mailbox uid required")
	}
	var resp apiErrorBody
	err := c.do("POST", "/api/mailboxes/cancel", CancelMailboxesRequest{UIDs: cleaned}, &resp)
	if err != nil {
		return err
	}
	return resp.errOrNil("failed to cancel mailbox")
}

type ForwardingRequest struct {
	UIDs            []string `json:"uids"`
	ForwardingEmail string   `json:"forwarding_email,omitempty"`
}

type ForwardingJob struct {
	ID     string `json:"id"`
	UID    string `json:"uid"`
	Status string `json:"status"`
	Email  string `json:"forwarding_email"`
}

type forwardingResponse struct {
	Error   bool            `json:"error"`
	Message string          `json:"message"`
	Jobs    []ForwardingJob `json:"jobs"`
	Result  []ForwardingJob `json:"result"`
	Data    []ForwardingJob `json:"data"`
}

func (r forwardingResponse) jobs() []ForwardingJob {
	if len(r.Jobs) > 0 {
		return r.Jobs
	}
	if len(r.Result) > 0 {
		return r.Result
	}
	return r.Data
}

func (c *Client) SetupForwarding(uids []string, forwardingEmail string) ([]ForwardingJob, error) {
	return c.forwardingOp("/api/mailboxes/forwarding/setup", uids, forwardingEmail, true)
}

func (c *Client) UpdateForwarding(uids []string, forwardingEmail string) ([]ForwardingJob, error) {
	return c.forwardingOp("/api/mailboxes/forwarding/update", uids, forwardingEmail, true)
}

func (c *Client) RemoveForwarding(uids []string) ([]ForwardingJob, error) {
	return c.forwardingOp("/api/mailboxes/forwarding/remove", uids, "", false)
}

func (c *Client) forwardingOp(path string, uids []string, forwardingEmail string, requireEmail bool) ([]ForwardingJob, error) {
	cleaned := make([]string, 0, len(uids))
	for _, u := range uids {
		u = strings.TrimSpace(u)
		if u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("at least one mailbox uid required")
	}
	forwardingEmail = strings.TrimSpace(forwardingEmail)
	if requireEmail && forwardingEmail == "" {
		return nil, fmt.Errorf("forwarding email required")
	}
	body := ForwardingRequest{UIDs: cleaned}
	if requireEmail {
		body.ForwardingEmail = forwardingEmail
	}
	var resp forwardingResponse
	err := c.do("POST", path, body, &resp)
	if err != nil {
		return nil, err
	}
	if err := (apiErrorBody{Error: resp.Error, Message: resp.Message}).errOrNil("forwarding request failed"); err != nil {
		return nil, err
	}
	return resp.jobs(), nil
}

func (c *Client) ListForwardingJobs(uids []string) ([]ForwardingJob, error) {
	body := map[string]any{}
	cleaned := make([]string, 0, len(uids))
	for _, u := range uids {
		u = strings.TrimSpace(u)
		if u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) > 0 {
		body["uids"] = cleaned
	}
	var resp forwardingResponse
	err := c.do("POST", "/api/mailboxes/forwarding/jobs", body, &resp)
	if err != nil {
		return nil, err
	}
	if err := (apiErrorBody{Error: resp.Error, Message: resp.Message}).errOrNil("failed to list forwarding jobs"); err != nil {
		return nil, err
	}
	return resp.jobs(), nil
}

// CheckMailboxStatus refreshes status for mailbox UIDs.
func (c *Client) CheckMailboxStatus(uids []string) ([]MailboxListItem, error) {
	cleaned := make([]string, 0, len(uids))
	for _, u := range uids {
		u = strings.TrimSpace(u)
		if u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("at least one mailbox uid required")
	}
	var resp mailboxListResponse
	err := c.do("POST", "/api/mailboxes/status", map[string]any{"uids": cleaned}, &resp)
	if err != nil {
		return nil, err
	}
	list := resp.Mailboxes
	if len(list) == 0 {
		list = resp.Data
	}
	for i := range list {
		if list[i].ID == "" {
			list[i].ID = list[i].ResolvedID()
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

// IsConnectOrderID marks domains connected via nameservers (not purchased orders).
func IsConnectOrderID(orderID string) bool {
	return strings.HasPrefix(orderID, "connect:")
}

func ConnectOrderID(uid, domain string) string {
	uid = strings.TrimSpace(uid)
	if uid != "" {
		return "connect:" + uid
	}
	return "connect:" + strings.ToLower(strings.TrimSpace(domain))
}
