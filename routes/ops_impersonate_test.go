package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func impersonationTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	r.Use(sessions.Sessions("emailtracker_session", store))
	r.POST("/ops/impersonate", RequireAuth(), RequireAdmin(), OpsImpersonate)
	r.POST("/ops/pilots", RequireAuth(), RequireAdmin(), OpsCreatePilot)
	r.POST("/impersonate/exit", RequireAuth(), ImpersonateExit)
	r.GET("/api/v1/auth/session", RequireAuth(), AuthSessionAPI)
	r.GET("/whoami", RequireAuth(), func(c *gin.Context) {
		id := mustUserID(c)
		imp, _ := auth.GetImpersonatorID(c)
		c.JSON(http.StatusOK, gin.H{"user_id": id, "impersonator_id": imp})
	})
	return r
}

func sessionCookie(t *testing.T, r *gin.Engine, userID int64) *http.Cookie {
	t.Helper()
	path := fmt.Sprintf("/__set_session_%d", userID)
	r.GET(path, func(c *gin.Context) {
		auth.SetUserSession(c, userID)
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "emailtracker_session" {
			return c
		}
	}
	t.Fatal("missing session cookie")
	return nil
}

func TestOpsImpersonateEnterAndExit(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = map[string]struct{}{"ops-admin@test.com": {}}
	config.BaseURL = "http://localhost"

	adminID, err := model.CreateUser("ops-admin@test.com", "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	clientEmail := fmt.Sprintf("ops-client-%d@test.com", time.Now().UnixNano())
	clientID, err := model.CreateUser(clientEmail, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}

	r := impersonationTestRouter(t)
	cookie := sessionCookie(t, r, adminID)

	form := url.Values{"user_email": {clientEmail}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ops/impersonate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("enter: code=%d loc=%s body=%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	var entered *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "emailtracker_session" {
			entered = c
		}
	}
	if entered == nil {
		entered = cookie
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req2.AddCookie(entered)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("whoami after enter: %d %s", w2.Code, w2.Body.String())
	}
	body := w2.Body.String()
	if !strings.Contains(body, fmt.Sprintf(`"user_id":%d`, clientID)) {
		t.Fatalf("expected effective user %d, got %s", clientID, body)
	}
	if !strings.Contains(body, fmt.Sprintf(`"impersonator_id":%d`, adminID)) {
		t.Fatalf("expected impersonator %d, got %s", adminID, body)
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/impersonate/exit", nil)
	req3.AddCookie(entered)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusFound || w3.Header().Get("Location") != "/ops/provisioning" {
		t.Fatalf("exit: code=%d loc=%s", w3.Code, w3.Header().Get("Location"))
	}
	var exited *http.Cookie
	for _, c := range w3.Result().Cookies() {
		if c.Name == "emailtracker_session" {
			exited = c
		}
	}
	if exited == nil {
		t.Fatal("expected session cookie after exit")
	}

	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req4.AddCookie(exited)
	r.ServeHTTP(w4, req4)
	body4 := w4.Body.String()
	if !strings.Contains(body4, fmt.Sprintf(`"user_id":%d`, adminID)) {
		t.Fatalf("expected admin restored, got %s", body4)
	}
	if !strings.Contains(body4, `"impersonator_id":0`) {
		t.Fatalf("expected impersonator cleared, got %s", body4)
	}
}

func TestOpsImpersonateRejectsNonAdminAndAdminTarget(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = map[string]struct{}{"ops-admin2@test.com": {}}
	config.BaseURL = "http://localhost"

	adminID, err := model.CreateUser("ops-admin2@test.com", "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	clientEmail := fmt.Sprintf("ops-plain-%d@test.com", time.Now().UnixNano())
	clientID, err := model.CreateUser(clientEmail, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	otherAdminEmail := fmt.Sprintf("ops-other-admin-%d@test.com", time.Now().UnixNano())
	otherAdminID, err := model.CreateUser(otherAdminEmail, "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = model.SetUserAdmin(otherAdminID, true)

	r := impersonationTestRouter(t)

	// Non-admin cannot enter.
	cookieClient := sessionCookie(t, r, clientID)
	form := url.Values{"user_email": {clientEmail}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ops/impersonate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieClient)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("non-admin expected redirect home, got %d %s", w.Code, w.Header().Get("Location"))
	}

	// Admin cannot enter another admin.
	cookieAdmin := sessionCookie(t, r, adminID)
	form2 := url.Values{"user_email": {otherAdminEmail}}
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/ops/impersonate", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(cookieAdmin)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusFound || !strings.Contains(w2.Header().Get("Location"), "error=") {
		t.Fatalf("enter admin expected error redirect, got %d %s", w2.Code, w2.Header().Get("Location"))
	}
}

func TestOpsCreatePilotAndEnter(t *testing.T) {
	db.OpenTestDB(t)
	config.AdminEmails = map[string]struct{}{"ops-admin3@test.com": {}}
	config.BaseURL = "http://localhost"

	adminID, err := model.CreateUser("ops-admin3@test.com", "hash", config.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("ops-pilot-%d@test.com", time.Now().UnixNano())

	r := impersonationTestRouter(t)
	cookie := sessionCookie(t, r, adminID)

	form := url.Values{"user_email": {email}, "enter_now": {"1"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ops/pilots", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("create+enter: code=%d loc=%s", w.Code, w.Header().Get("Location"))
	}

	u, err := model.GetUserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	if model.NormalizePlanTier(u.PlanTier) != model.PlanTierPro || u.SubscriptionStatus != model.SubStatusActive {
		t.Fatalf("pilot not granted: tier=%s status=%s", u.PlanTier, u.SubscriptionStatus)
	}

	var entered *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "emailtracker_session" {
			entered = c
		}
	}
	if entered == nil {
		t.Fatal("missing session after enter")
	}
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req2.AddCookie(entered)
	r.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), fmt.Sprintf(`"user_id":%d`, u.ID)) {
		t.Fatalf("expected working as pilot, got %s", w2.Body.String())
	}
}
