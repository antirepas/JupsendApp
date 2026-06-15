package routes

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emailtracker.com/db"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	r.Use(sessions.Sessions("emailtracker_session", store))
	r.GET("/login", LoginPage)
	r.GET("/signup", SignupPage)
	r.POST("/signup", SignupSubmit)
	r.POST("/login", LoginSubmit)
	r.POST("/logout", Logout)
	return r
}

func TestSignupLoginLogout(t *testing.T) {
	mem, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.DB = mem
	db.CreateTables()
	defer mem.Close()

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
	if !strings.Contains(w.Header().Get("Location"), "/settings") {
		t.Fatalf("expected redirect to settings, got %s", w.Header().Get("Location"))
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logout.Header.Set("Cookie", w.Header().Get("Set-Cookie"))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, logout)
	if w2.Code != http.StatusFound || !strings.Contains(w2.Header().Get("Location"), "/login") {
		t.Fatalf("logout failed: %d %s", w2.Code, w2.Header().Get("Location"))
	}

	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(
		fmt.Sprintf("email=%s&password=password123", email),
	))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, login)
	if w3.Code != http.StatusFound || w3.Header().Get("Location") != "/" {
		t.Fatalf("login failed: %d %s", w3.Code, w3.Header().Get("Location"))
	}
}
