package whop

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/model"
)

func apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(config.WhopAPIBase), "/")
	if base == "" {
		return "https://api.whop.com/api/v1"
	}
	return base
}

type CheckoutResponse struct {
	PurchaseURL string `json:"purchase_url"`
}

func CreateCheckout(userID int64, tier model.PlanTier, redirectURL string) (string, error) {
	if config.WhopAPIKey == "" || config.WhopCompanyID == "" {
		return "", fmt.Errorf("whop not configured: set WHOP_API_KEY and WHOP_COMPANY_ID")
	}
	planID, err := resolvePlanIDForTier(tier)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{
		"mode":         "payment",
		"plan_id":      planID,
		"redirect_url": redirectURL,
		"metadata": map[string]string{
			"user_id": strconv.FormatInt(userID, 10),
			"plan_tier": string(tier),
		},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, apiBase()+"/checkout_configurations", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+config.WhopAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("whop checkout %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		PurchaseURL string `json:"purchase_url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if out.PurchaseURL == "" {
		return "", fmt.Errorf("empty purchase_url from whop")
	}
	return out.PurchaseURL, nil
}

func resolvePlanIDForTier(tier model.PlanTier) (string, error) {
	switch tier {
	case model.PlanTierPro:
		return resolvePlanIDFromConfig(config.WhopPlanIDPro, config.WhopPlanID, config.WhopProductID)
	case model.PlanTierStandard:
		return resolvePlanIDFromConfig(config.WhopPlanIDStandard, config.WhopPlanID, config.WhopProductID)
	default:
		return resolvePlanIDFromConfig(config.WhopPlanID, config.WhopPlanID, config.WhopProductID)
	}
}

func resolvePlanIDFromConfig(planID, fallbackPlanID, productID string) (string, error) {
	planID = strings.TrimSpace(planID)
	productID = strings.TrimSpace(productID)
	fallbackPlanID = strings.TrimSpace(fallbackPlanID)

	if strings.HasPrefix(planID, "prod_") {
		productID = planID
		planID = ""
	}
	if planID != "" && strings.HasPrefix(planID, "plan_") {
		return planID, nil
	}
	if planID != "" && !strings.HasPrefix(planID, "plan_") {
		return "", fmt.Errorf("WHOP_PLAN_ID must start with plan_ (got %q). Use WHOP_PRODUCT_ID for prod_ IDs", planID)
	}
	if planID == "" && fallbackPlanID != "" {
		planID = strings.TrimSpace(fallbackPlanID)
		if strings.HasPrefix(planID, "plan_") {
			return planID, nil
		}
		// If fallbackPlanID was a prod_ style, allow it too.
		if strings.HasPrefix(planID, "prod_") {
			productID = planID
			planID = ""
		}
	}
	if productID == "" {
		return "", fmt.Errorf("set WHOP_PLAN_ID (plan_...) or WHOP_PRODUCT_ID (prod_...)")
	}
	return firstPlanForProduct(config.WhopCompanyID, productID)
}

func firstPlanForProduct(companyID, productID string) (string, error) {
	q := url.Values{}
	q.Set("account_id", companyID)
	q.Set("product_ids", productID)
	q.Set("first", "1")
	req, err := http.NewRequest(http.MethodGet, apiBase()+"/plans?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+config.WhopAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("whop list plans %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 || out.Data[0].ID == "" {
		return "", fmt.Errorf("no plan found for product %s — create a pricing plan on that product in Whop", productID)
	}
	return out.Data[0].ID, nil
}

type WebhookEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Timestamp string          `json:"timestamp"`
}

type MembershipData struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	Member             *struct {
		ID string `json:"id"`
	} `json:"member"`
	User *struct {
		Email string `json:"email"`
	} `json:"user"`
	Metadata           map[string]interface{} `json:"metadata"`
	RenewalPeriodEnd   *time.Time             `json:"renewal_period_end"`
	Valid              bool                   `json:"valid"`
}

func VerifyWebhookSignature(body []byte, headers http.Header) bool {
	secret := config.WhopWebhookSecret
	if secret == "" {
		return false
	}
	secret = strings.TrimPrefix(secret, "whsec_")
	msgID := headers.Get("webhook-id")
	ts := headers.Get("webhook-timestamp")
	sig := headers.Get("webhook-signature")
	if msgID == "" || ts == "" || sig == "" {
		return false
	}
	payload := msgID + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, part := range strings.Split(sig, " ") {
		part = strings.TrimPrefix(part, "v1,")
		if hmac.Equal([]byte(part), []byte(expected)) {
			return true
		}
	}
	return sig == expected
}

func ParseMembershipActivated(data json.RawMessage) (MembershipData, error) {
	var m MembershipData
	err := json.Unmarshal(data, &m)
	return m, err
}

func UserIDFromMetadata(meta map[string]interface{}) int64 {
	if meta == nil {
		return 0
	}
	switch v := meta["user_id"].(type) {
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	case float64:
		return int64(v)
	default:
		return 0
	}
}
