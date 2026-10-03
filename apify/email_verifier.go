package apify

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
	"emailtracker.com/model"
)

const (
	apiBaseURL     = "https://api.apify.com/v2"
	waitForFinishS = 120
	chunkTimeout   = 180 * time.Second
)

// Client talks to the Apify REST API for email verification.
type Client struct {
	Token  string
	Actor  string
	HTTP   *http.Client
	BaseURL string
}

func NewClient() *Client {
	actor := config.ApifyEmailVerifierActor
	if actor == "" {
		actor = "account56~email-verifier"
	}
	return &Client{
		Token:   config.ApifyToken,
		Actor:   actor,
		BaseURL: apiBaseURL,
		HTTP:    &http.Client{Timeout: chunkTimeout},
	}
}

func Configured() bool {
	return config.ApifyConfigured()
}

// VerifyEmails runs the email-verifier Actor and returns dataset items.
func VerifyEmails(emails []string) ([]model.EmailVerificationResult, error) {
	return NewClient().VerifyEmails(emails)
}

func (c *Client) VerifyEmails(emails []string) ([]model.EmailVerificationResult, error) {
	if c == nil || strings.TrimSpace(c.Token) == "" {
		return nil, fmt.Errorf("APIFY_TOKEN is not configured")
	}
	clean := make([]string, 0, len(emails))
	seen := map[string]bool{}
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		clean = append(clean, e)
	}
	if len(clean) == 0 {
		return nil, nil
	}

	actor := c.Actor
	if actor == "" {
		actor = "account56~email-verifier"
	}
	actorPath := url.PathEscape(actor)
	q := url.Values{}
	q.Set("token", c.Token)
	q.Set("waitForFinish", fmt.Sprintf("%d", waitForFinishS))
	runURL := fmt.Sprintf("%s/actors/%s/runs?%s", strings.TrimRight(c.BaseURL, "/"), actorPath, q.Encode())

	body, _ := json.Marshal(map[string]interface{}{"emails": clean})
	req, err := http.NewRequest(http.MethodPost, runURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apify run: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apify run HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var runEnvelope struct {
		Data struct {
			ID               string `json:"id"`
			Status           string `json:"status"`
			DefaultDatasetID string `json:"defaultDatasetId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &runEnvelope); err != nil {
		return nil, fmt.Errorf("apify run decode: %w", err)
	}
	datasetID := runEnvelope.Data.DefaultDatasetID
	status := strings.ToUpper(runEnvelope.Data.Status)
	if datasetID == "" {
		return nil, fmt.Errorf("apify run missing dataset (status=%s)", runEnvelope.Data.Status)
	}
	if status != "" && status != "SUCCEEDED" && status != "RUNNING" && status != "READY" {
		// Still try dataset if finished with items; otherwise fail.
		if status == "FAILED" || status == "ABORTED" || status == "TIMED-OUT" {
			return nil, fmt.Errorf("apify run %s", strings.ToLower(status))
		}
	}

	// If waitForFinish expired while still running, poll briefly.
	if status == "RUNNING" || status == "READY" {
		if err := c.waitRun(runEnvelope.Data.ID, 60*time.Second); err != nil {
			return nil, err
		}
	}

	return c.fetchDataset(datasetID)
}

func (c *Client) waitRun(runID string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		q := url.Values{}
		q.Set("token", c.Token)
		u := fmt.Sprintf("%s/actor-runs/%s?%s", strings.TrimRight(c.BaseURL, "/"), url.PathEscape(runID), q.Encode())
		resp, err := c.HTTP.Get(u)
		if err != nil {
			return err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var env struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &env)
		st := strings.ToUpper(env.Data.Status)
		switch st {
		case "SUCCEEDED":
			return nil
		case "FAILED", "ABORTED", "TIMED-OUT":
			return fmt.Errorf("apify run %s", strings.ToLower(st))
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("apify run timed out waiting for finish")
}

func (c *Client) fetchDataset(datasetID string) ([]model.EmailVerificationResult, error) {
	q := url.Values{}
	q.Set("token", c.Token)
	q.Set("clean", "true")
	q.Set("format", "json")
	q.Set("limit", "10000")
	u := fmt.Sprintf("%s/datasets/%s/items?%s", strings.TrimRight(c.BaseURL, "/"), url.PathEscape(datasetID), q.Encode())
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, fmt.Errorf("apify dataset: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apify dataset HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var items []struct {
		Email      string `json:"email"`
		Result     string `json:"result"`
		Subresult  string `json:"subresult"`
		Quality    string `json:"quality"`
		Role       bool   `json:"role"`
		Free       bool   `json:"free"`
		DidYouMean string `json:"didyoumean"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("apify dataset decode: %w", err)
	}
	out := make([]model.EmailVerificationResult, 0, len(items))
	for _, it := range items {
		out = append(out, model.EmailVerificationResult{
			Email:      it.Email,
			Result:     it.Result,
			Subresult:  it.Subresult,
			Quality:    it.Quality,
			Role:       it.Role,
			Free:       it.Free,
			DidYouMean: it.DidYouMean,
			Error:      it.Error,
		})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
