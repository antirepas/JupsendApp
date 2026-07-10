package routes

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const oauthStateSessionKey = "gmail_oauth_state"

func GmailConnect(c *gin.Context) {
	userID := mustUserID(c)
	if !googleoauth.IsConfigured() {
		c.Redirect(http.StatusFound, "/settings?error=Gmail+OAuth+not+configured")
		return
	}
	nonce := randomNonce()
	state := googleoauth.EncodeState(userID, nonce)
	s := sessions.Default(c)
	s.Set(oauthStateSessionKey, nonce)
	_ = s.Save()
	c.Redirect(http.StatusFound, googleoauth.AuthURL(state))
}

func GmailCallback(c *gin.Context) {
	userID := mustUserID(c)
	if errMsg := c.Query("error"); errMsg != "" {
		c.Redirect(http.StatusFound, "/settings?error=Gmail+connection+cancelled")
		return
	}
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.Redirect(http.StatusFound, "/settings?error=Invalid+OAuth+response")
		return
	}
	stateUserID, nonce, err := googleoauth.DecodeState(state)
	if err != nil || stateUserID != userID {
		c.Redirect(http.StatusFound, "/settings?error=Invalid+OAuth+state")
		return
	}
	s := sessions.Default(c)
	expected, _ := s.Get(oauthStateSessionKey).(string)
	s.Delete(oauthStateSessionKey)
	_ = s.Save()
	if expected == "" || expected != nonce {
		c.Redirect(http.StatusFound, "/settings?error=OAuth+session+expired")
		return
	}

	tok, profile, err := googleoauth.ExchangeCode(c.Request.Context(), code)
	if err != nil || profile.Email == "" {
		c.Redirect(http.StatusFound, "/settings?error=Could+not+complete+Gmail+connection")
		return
	}
	if tok.RefreshToken == "" {
		c.Redirect(http.StatusFound, "/settings?error=Google+did+not+return+a+refresh+token.+Revoke+app+access+in+Google+Account+and+try+again")
		return
	}
	encRefresh, err := googleoauth.Encrypt(tok.RefreshToken)
	if err != nil {
		c.Redirect(http.StatusFound, "/settings?error=Could+not+store+Gmail+token")
		return
	}
	encAccess, _ := googleoauth.Encrypt(tok.AccessToken)
	fromName := profile.Name
	if fromName == "" {
		fromName = profile.Email
	}
	if err := model.SaveGoogleOAuthAccount(userID, profile.Email, fromName, encRefresh, encAccess, tok.Expiry); err != nil {
		c.Redirect(http.StatusFound, "/settings?error=Could+not+save+Gmail+account")
		return
	}
	c.Redirect(http.StatusFound, "/settings?success=Gmail+connected")
}

func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
