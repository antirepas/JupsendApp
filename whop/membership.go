package whop

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

// MembershipDetails is a subset of Whop membership fields used by billing UI.
type MembershipDetails struct {
	ID               string
	Status           string
	ManageURL        string
	CancelAtPeriodEnd bool
	RenewalPeriodEnd *time.Time
	PlanID           string
}

func GetMembership(membershipID string) (MembershipDetails, error) {
	membershipID = strings.TrimSpace(membershipID)
	if membershipID == "" {
		return MembershipDetails{}, fmt.Errorf("membership id required")
	}
	req, err := http.NewRequest(http.MethodGet, apiBase()+"/memberships/"+membershipID, nil)
	if err != nil {
		return MembershipDetails{}, err
	}
	req.Header.Set("Authorization", "Bearer "+config.WhopAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return MembershipDetails{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return MembershipDetails{}, fmt.Errorf("whop membership %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw struct {
		ID                string `json:"id"`
		Status            string `json:"status"`
		ManageURL         string `json:"manage_url"`
		CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
		RenewalPeriodEnd  string `json:"renewal_period_end"`
		Plan              struct {
			ID string `json:"id"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return MembershipDetails{}, err
	}
	out := MembershipDetails{
		ID:                raw.ID,
		Status:            raw.Status,
		ManageURL:         raw.ManageURL,
		CancelAtPeriodEnd: raw.CancelAtPeriodEnd,
		PlanID:            raw.Plan.ID,
	}
	if raw.RenewalPeriodEnd != "" {
		if t, err := time.Parse(time.RFC3339, raw.RenewalPeriodEnd); err == nil {
			out.RenewalPeriodEnd = &t
		}
	}
	return out, nil
}

// CancelMembership cancels at period end by default (user keeps access until renewal date).
func CancelMembership(membershipID string) error {
	membershipID = strings.TrimSpace(membershipID)
	if membershipID == "" {
		return fmt.Errorf("membership id required")
	}
	payload := map[string]string{"cancellation_mode": "at_period_end"}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, apiBase()+"/memberships/"+membershipID+"/cancel", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.WhopAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("whop cancel %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
