package routes

import (
	"emailtracker.com/auth"
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
