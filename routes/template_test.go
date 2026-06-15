package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestSaveTemplate_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(
		http.MethodPost,
		"/template",
		bytes.NewBufferString(`{bad json`),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	SaveTemplate(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}

	expected := `{"message":"could not get request body"}`
	if w.Body.String() != expected {
		t.Fatalf("expected %s, got %s", expected, w.Body.String())
	}
}

func TestSaveTemplate_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.Prepare()

	payload := ST{
		T: model.Template{
			Name:    "Template1",
			Subject: "Test",
			Body:    "This is test number {{number}}!",
		},
		TV: []model.TemplateVariable{
			{
				Key: "number",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(
		http.MethodPost,
		"/template",
		bytes.NewBuffer(body),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, 1)

	SaveTemplate(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf(
			"expected %d, got %d, body=%s",
			http.StatusOK,
			w.Code,
			w.Body.String(),
		)
	}

	expected := `{"message":"template saved successfully!"}`
	if w.Body.String() != expected {
		t.Fatalf("expected %s, got %s", expected, w.Body.String())
	}
}
