package routes

import (
	"net/http"
	"net/url"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/googleoauth"
	"github.com/gin-gonic/gin"
)

func LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "auth_login.html", gin.H{
		"title":            "Login",
		"error":            c.Query("error"),
		"next":             c.Query("next"),
		"googleConfigured": googleoauth.IsConfigured(),
	})
}

func SignupPage(c *gin.Context) {
	// Plan-first onboarding: direct users to plan selection.
	c.Redirect(http.StatusFound, "/signup/free")
}

func SignupSubmit(c *gin.Context) {
	// Email/password signup is disabled in favor of plan-first Google onboarding.
	c.Redirect(http.StatusFound, "/signup/free?error=Choose+a+plan+and+continue+with+Google")
}

func Logout(c *gin.Context) {
	auth.ClearSession(c)
	c.Redirect(http.StatusFound, "/login")
}

func safeNext(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" {
		return "/"
	}
	return raw
}
