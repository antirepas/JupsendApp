package routes

import (
	"net/http"
	"net/url"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "auth_login.html", gin.H{
		"title": "Login",
		"error": c.Query("error"),
		"next":  c.Query("next"),
	})
}

func SignupPage(c *gin.Context) {
	c.HTML(http.StatusOK, "auth_signup.html", gin.H{
		"title": "Sign up",
		"error": c.Query("error"),
	})
}

func LoginSubmit(c *gin.Context) {
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	next := c.PostForm("next")

	user, err := model.GetUserByEmail(email)
	if err != nil || !auth.CheckPassword(user.PasswordHash, password) {
		c.Redirect(http.StatusFound, "/login?error=Invalid+email+or+password")
		return
	}
	auth.SetUserSession(c, user.ID)
	_ = model.SetUserAdmin(user.ID, config.IsAdminEmail(user.Email))
	c.Redirect(http.StatusFound, safeNext(next))
}

func SignupSubmit(c *gin.Context) {
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	confirm := c.PostForm("confirm_password")

	if email == "" || len(password) < 8 {
		c.Redirect(http.StatusFound, "/signup?error=Email+and+password+(8%2B+chars)+required")
		return
	}
	if password != confirm {
		c.Redirect(http.StatusFound, "/signup?error=Passwords+do+not+match")
		return
	}
	exists, _ := model.EmailExists(email)
	if exists {
		c.Redirect(http.StatusFound, "/signup?error=Email+already+registered")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup?error=Could+not+create+account")
		return
	}

	userID, err := model.CreateUser(email, hash, config.BaseURL)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup?error=Could+not+create+account")
		return
	}
	_ = model.AssignOrphanDataToUser(userID)
	_ = model.CreateDefaultSMTPAccountForUser(userID)
	if config.IsAdminEmail(email) {
		_ = model.SetUserAdmin(userID, true)
	}

	auth.SetUserSession(c, userID)
	c.Redirect(http.StatusFound, "/settings/billing?success=Welcome%21+Choose+a+plan+to+unlock+the+app")
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
