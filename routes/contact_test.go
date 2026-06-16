package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestSaveContacts_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "/contacts", bytes.NewBufferString(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	SaveContacts(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	expected := `{"message":"could not get request body"}`
	if w.Body.String() != expected {
		t.Fatalf("expected body %s, got %s", expected, w.Body.String())
	}
}

func TestSaveContacts_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	userID, err := model.CreateUser(
		fmt.Sprintf("contact-test-%d@test.com", time.Now().UnixNano()),
		"hash",
		"http://localhost",
	)
	if err != nil {
		t.Fatal(err)
	}

	email := fmt.Sprintf("save-contact-%d@example.com", time.Now().UnixNano())

	payload := SC{
		CS: []model.Contact{
			{
				ID:    1,
				Email: email,
			},
		},
		CVS: []model.ContactVariables{
			{
				ContactID: 1,
				Key:       "number",
				Value:     "1",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "/contacts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	setTestUser(ctx, userID)

	SaveContacts(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM contact WHERE email = ? AND user_id = ?", email, userID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("expected 1 saved contact, got %d", count)
	}
}
