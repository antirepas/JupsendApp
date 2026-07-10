package routes

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	ensureTestGoogleOAuthConfig()
	r := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	r.Use(sessions.Sessions("emailtracker_session", store))
	r.SetFuncMap(template.FuncMap{
		"assetURL": func(path string) string {
			return path + "?v=" + util.StaticAssetVersion()
		},
	})
	r.LoadHTMLFiles(
		"../templates/partials/head.html",
		"../templates/auth_login.html",
	)
	r.GET("/login", LoginPage)
	r.GET("/signup", SignupPage)
	r.POST("/signup", SignupSubmit)
	r.POST("/logout", Logout)
	return r
}

func ensureTestGoogleOAuthConfig() {
	if config.GoogleClientID != "" {
		return
	}
	config.GoogleClientID = "test-client-id"
	config.GoogleClientSecret = "test-client-secret"
	config.GoogleOAuthRedirectURI = "http://localhost/settings/gmail/callback"
	config.GoogleAppOAuthRedirectURI = "http://localhost/auth/google/callback"
}

func TestLoginPageRenders(t *testing.T) {
	r := testRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login page expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Continue with Google") {
		t.Fatalf("login page should offer Google sign-in")
	}
}

func TestSignupRedirectAndLogout(t *testing.T) {
	db.OpenTestDB(t)

	r := testRouter()
	email := fmt.Sprintf("auth-test-%d@test.com", time.Now().UnixNano())

	signup := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(
		fmt.Sprintf("email=%s&password=password123&confirm_password=password123", email),
	))
	signup.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, signup)
	if w.Code != http.StatusFound {
		t.Fatalf("signup expected redirect, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Location"), "/signup/free") {
		t.Fatalf("expected redirect to /signup/free, got %s", w.Header().Get("Location"))
	}

	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	config.BaseURL = "http://localhost"
	if _, err := model.CreateUser(email, hash, config.BaseURL); err != nil {
		t.Fatalf("create user: %v", err)
	}

	loginPage := httptest.NewRequest(http.MethodGet, "/login", nil)
	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, loginPage)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login page expected 200, got %d", wLogin.Code)
	}
	if !strings.Contains(wLogin.Body.String(), "Continue with Google") {
		t.Fatalf("login page should offer Google sign-in")
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logout.Header.Set("Cookie", w.Header().Get("Set-Cookie"))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, logout)
	if w2.Code != http.StatusFound || !strings.Contains(w2.Header().Get("Location"), "/login") {
		t.Fatalf("logout failed: %d %s", w2.Code, w2.Header().Get("Location"))
	}
}
