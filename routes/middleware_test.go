package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func subscriptionTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	r.Use(sessions.Sessions("emailtracker_session", store))
	r.GET("/protected", RequireAuth(), RequireSubscription(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.GET("/settings/billing", RequireAuth(), BillingPage)
	return r
}

func TestRequireSubscriptionRedirectsUnsubscribed(t *testing.T) {
	db.OpenTestDB(t)

	email := fmt.Sprintf("sub-test-%d@test.com", time.Now().UnixNano())
	hash, _ := auth.HashPassword("password123")
	userID, err := model.CreateUser(email, hash, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}

	r := subscriptionTestRouter()
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusFound || !strings.Contains(w2.Header().Get("Location"), "/login") {
		t.Fatalf("expected login redirect without session, got %d %s", w2.Code, w2.Header().Get("Location"))
	}

	sessReq := httptest.NewRequest(http.MethodGet, "/", nil)
	sessW := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(sessW)
	c2.Request = sessReq
	store := cookie.NewStore([]byte("test-session-secret"))
	sessions.Sessions("emailtracker_session", store)(c2)
	auth.SetUserSession(c2, userID)
	cookieHeader := sessW.Header().Get("Set-Cookie")

	loginReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	loginReq.Header.Set("Cookie", cookieHeader)
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusFound || !strings.Contains(loginW.Header().Get("Location"), "/settings/billing") {
		t.Fatalf("expected billing redirect, got %d %s", loginW.Code, loginW.Header().Get("Location"))
	}

	_ = model.UpdateUserSubscription(userID, model.SubStatusActive, "mem_test", "mbr_test", nil)
	okReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	okReq.Header.Set("Cookie", cookieHeader)
	okW := httptest.NewRecorder()
	r.ServeHTTP(okW, okReq)
	if okW.Code != http.StatusOK {
		t.Fatalf("expected 200 with active subscription, got %d", okW.Code)
	}
}

func TestRequireSubscriptionAllowsAdminEmail(t *testing.T) {
	db.OpenTestDB(t)

	adminEmail := fmt.Sprintf("admin-%d@test.com", time.Now().UnixNano())
	config.AdminEmails = map[string]struct{}{strings.ToLower(adminEmail): {}}

	hash, _ := auth.HashPassword("password123")
	userID, err := model.CreateUser(adminEmail, hash, "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}

	r := subscriptionTestRouter()
	sessReq := httptest.NewRequest(http.MethodGet, "/", nil)
	sessW := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(sessW)
	c2.Request = sessReq
	store := cookie.NewStore([]byte("test-session-secret"))
	sessions.Sessions("emailtracker_session", store)(c2)
	auth.SetUserSession(c2, userID)
	cookieHeader := sessW.Header().Get("Set-Cookie")

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cookie", cookieHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected admin to access app, got %d", w.Code)
	}

	config.AdminEmails = nil
}
