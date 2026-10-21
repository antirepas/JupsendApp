package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"emailtracker.com/config"
)

const jevAPIURL = "https://api.typesafe.ai/v1/systemone"

// JevClient calls TypeSafe System One (Jev).
type JevClient struct {
	APIKey string
	Model  string
	HTTP   *http.Client
	BaseURL string
}

func NewJevClient() *JevClient {
	model := strings.TrimSpace(config.JEVModel)
	if model == "" {
		model = "jev-latest"
	}
	return &JevClient{
		APIKey:  config.JEVAPIKey,
		Model:   model,
		BaseURL: jevAPIURL,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

func JEVConfigured() bool {
	return config.JEVEnabled()
}

// JevNoulAnswer is a yes/no probability.
type JevNoulAnswer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

// JevChoiceAnswer is a discrete choice with confidence + distribution.
type JevChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// JevEvaluateResponse is the System One response body.
type JevEvaluateResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Evaluate posts state + questions to Jev.
func (c *JevClient) Evaluate(ctx context.Context, state interface{}, questions map[string]interface{}) (*JevEvaluateResponse, error) {
	if c == nil || strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("JEV_API_KEY is not configured")
	}
	model := c.Model
	if model == "" {
		model = "jev-latest"
	}
	body, err := json.Marshal(map[string]interface{}{
		"model":     model,
		"state":     state,
		"questions": questions,
	})
	if err != nil {
		return nil, err
	}
	url := c.BaseURL
	if url == "" {
		url = jevAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jev request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("jev HTTP %d: %s", resp.StatusCode, msg)
	}
	var out JevEvaluateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("jev decode: %w", err)
	}
	if out.Answers == nil {
		out.Answers = map[string]json.RawMessage{}
	}
	return &out, nil
}

func DecodeJevChoice(raw json.RawMessage) (JevChoiceAnswer, error) {
	var a JevChoiceAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, err
	}
	return a, nil
}

func DecodeJevNoul(raw json.RawMessage) (JevNoulAnswer, error) {
	var a JevNoulAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, err
	}
	return a, nil
}
