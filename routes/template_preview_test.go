package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPreviewTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	payload, _ := json.Marshal(map[string]interface{}{
		"subject": "Hello {{name}}",
		"body":    "<p>Welcome to {{company}}</p>",
		"sample": map[string]string{
			"name":    "Alex",
			"company": "Acme Corp",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/templates/preview", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	PreviewTemplate(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Subject  string `json:"subject"`
		BodyHTML string `json:"body_html"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Subject != "Hello Alex" {
		t.Fatalf("subject=%q", resp.Subject)
	}
	if !strings.Contains(resp.BodyHTML, "Welcome to Acme Corp") {
		t.Fatalf("body=%q", resp.BodyHTML)
	}
	if !strings.Contains(strings.ToLower(resp.BodyHTML), "<html") {
		t.Fatalf("expected wrapped html body")
	}
}

func TestPreviewTemplateBadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/templates/preview", bytes.NewBufferString(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	PreviewTemplate(ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
