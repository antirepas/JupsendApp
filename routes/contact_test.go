package routes

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	NewDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	db.DB = NewDB
	db.CreateTables()

	_, err = NewDB.Exec("DELETE FROM contact_variables")
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewDB.Exec("DELETE FROM contact")
	if err != nil {
		t.Fatal(err)
	}

	// 4. Build request body
	payload := SC{
		CS: []model.Contact{
			{
				ID:    1,
				Email: "test@example.com",
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
	setTestUser(ctx, 1)

	// 5. Call handler
	SaveContacts(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// 6. Verify data actually exists in DB
	var count int
	err = NewDB.QueryRow("SELECT COUNT(*) FROM contact WHERE email = ?", "test@example.com").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("expected 1 saved contact, got %d", count)
	}
}
