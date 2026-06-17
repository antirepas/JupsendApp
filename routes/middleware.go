package routes

import (
	"net/http"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func mustUserID(c *gin.Context) int64 {
	if v, ok := c.Get("userID"); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	id, ok := auth.GetUserID(c)
	if ok {
		c.Set("userID", id)
		return id
	}
	return 0
}

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := auth.RequireUserID(c); !ok {
			return
		}
		c.Next()
	}
}

func RedirectIfAuthed() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, ok := auth.GetUserID(c); ok && id > 0 {
			c.Redirect(302, "/")
			c.Abort()
			return
		}
		c.Next()
	}
}

func subscriptionExempt(path string) bool {
	if path == "/settings" || path == "/settings/billing" {
		return true
	}
	if strings.HasPrefix(path, "/settings/gmail/") {
		return true
	}
	return false
}

func RequireSubscription() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := mustUserID(c)
		if userID == 0 {
			return
		}
		if subscriptionExempt(c.Request.URL.Path) {
			c.Next()
			return
		}
		user, err := model.GetUserByID(userID)
		if err != nil {
			auth.ClearSession(c)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		if model.UserHasAppAccess(user) {
			c.Set("user", user)
			c.Next()
			return
		}
		if auth.IsAPI(c) {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"message": "active subscription required"})
			return
		}
		c.Redirect(http.StatusFound, "/settings/billing?error=Subscription+required")
		c.Abort()
	}
}
