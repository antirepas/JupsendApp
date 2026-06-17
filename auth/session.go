package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const sessionUserKey = "user_id"

func SetUserSession(c *gin.Context, userID int64) {
	s := sessions.Default(c)
	s.Set(sessionUserKey, userID)
	_ = s.Save()
}

func GetUserID(c *gin.Context) (int64, bool) {
	s := sessions.Default(c)
	v := s.Get(sessionUserKey)
	if v == nil {
		return 0, false
	}
	switch id := v.(type) {
	case int64:
		return id, id > 0
	case int:
		return int64(id), id > 0
	case float64:
		return int64(id), id > 0
	default:
		return 0, false
	}
}

func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
}

func RequireUserID(c *gin.Context) (int64, bool) {
	id, ok := GetUserID(c)
	if !ok {
		if isAPI(c) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "authentication required"})
		} else {
			next := c.Request.URL.Path
			if c.Request.URL.RawQuery != "" {
				next += "?" + c.Request.URL.RawQuery
			}
			c.Redirect(http.StatusFound, "/login?next="+next)
		}
		return 0, false
	}
	c.Set("userID", id)
	return id, true
}

func isAPI(c *gin.Context) bool {
	return len(c.Request.URL.Path) >= 8 && c.Request.URL.Path[:8] == "/api/v1/"
}

func IsAPI(c *gin.Context) bool {
	return isAPI(c)
}
