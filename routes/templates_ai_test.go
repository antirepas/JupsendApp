package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"emailtracker.com/ai"
	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

type mockCompleter struct {
	response string
	err      error
}

func (m *mockCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestTemplateLint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	payload, _ := json.Marshal(map[string]string{
		"subject": "Hello",
		"body":    "<p>Plain</p>",
	})
	req := httptest.NewRequest(http.MethodPost, "/templates/lint", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	TemplateLint(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Lint []struct {
			Code string `json:"code"`
		} `json:"lint"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range resp.Lint {
		if issue.Code == "no_personalization" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected no_personalization in lint response")
	}
}

func TestTemplateAIRewriteRejectsLongText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.OpenAIAPIKey = "test-key"
	t.Cleanup(func() {
		config.OpenAIAPIKey = ""
		ai.ResetRateLimits()
	})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	longText := make([]byte, 51)
	for i := range longText {
		longText[i] = 'a'
	}
	payload, _ := json.Marshal(map[string]string{
		"text":   string(longText),
		"action": "shorten",
	})
	req := httptest.NewRequest(http.MethodPost, "/templates/ai/rewrite", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	TemplateAIRewrite(ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTemplateAIRewriteWhenAIDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.OpenAIAPIKey = ""
	t.Cleanup(func() { ai.ResetRateLimits() })

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	payload, _ := json.Marshal(map[string]string{
		"text":   "Hello there",
		"action": "shorten",
	})
	req := httptest.NewRequest(http.MethodPost, "/templates/ai/rewrite", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	TemplateAIRewrite(ctx)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTemplateAIRewriteSuccess(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("ai-rewrite-%d@test.com", time.Now().UnixNano())
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	userID, err := model.CreateUser(email, hash, "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ApplyPlanLimitsToUser(userID, model.PlanTierFree); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	config.OpenAIAPIKey = "test-key"
	orig := templateAICompleter
	templateAICompleter = &mockCompleter{response: "Hi"}
	t.Cleanup(func() {
		templateAICompleter = orig
		config.OpenAIAPIKey = ""
		ai.ResetRateLimits()
	})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	payload, _ := json.Marshal(map[string]string{
		"text":   "Hello there friend",
		"action": "shorten",
	})
	req := httptest.NewRequest(http.MethodPost, "/templates/ai/rewrite", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, userID)

	TemplateAIRewrite(ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hi" {
		t.Fatalf("text=%q", resp.Text)
	}
}
