package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestVerifyContactListStubbed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	email := fmt.Sprintf("list-verify-%d@test.com", time.Now().UnixNano())
	userID, err := model.CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	listID, err := model.CreateContactList(userID, "Verify list")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: fmt.Sprintf("member-%d@example.com", time.Now().UnixNano())}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AddContactsToList(listID, userID, []int64{cid}); err != nil {
		t.Fatal(err)
	}

	origToken := config.ApifyToken
	origFn := emailVerifyFn
	config.ApifyToken = "test-token"
	emailVerifyFn = func(emails []string) ([]model.EmailVerificationResult, error) {
		out := make([]model.EmailVerificationResult, 0, len(emails))
		for _, e := range emails {
			out = append(out, model.EmailVerificationResult{Email: e, Result: "ok", Subresult: "deliverable"})
		}
		return out, nil
	}
	t.Cleanup(func() {
		config.ApifyToken = origToken
		emailVerifyFn = origFn
	})

	router := gin.New()
	router.POST("/contacts/lists/:id/verify", func(ctx *gin.Context) {
		setTestUser(ctx, userID)
		VerifyContactList(ctx)
	})

	form := url.Values{}
	form.Set("only_unverified", "1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d body=%q", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "success=") || !strings.Contains(loc, "Verified") {
		t.Fatalf("location=%q", loc)
	}
	st, _, err := model.GetContactEmailStatus(cid)
	if err != nil || st != "valid" {
		t.Fatalf("status=%q err=%v", st, err)
	}
}
