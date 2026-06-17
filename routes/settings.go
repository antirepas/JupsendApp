package routes

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func SettingsPage(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	acc, _ := model.GetSMTPAccountByUserID(userID)
	c.HTML(http.StatusOK, "settings.html", gin.H{
		"title":          "Settings",
		"active":         "settings",
		"user":           user,
		"account":        acc,
		"gmailConnected": acc.IsGoogleOAuth(),
		"gmailConfigured": googleoauth.IsConfigured(),
		"subscribed":     model.UserHasActiveSubscription(user),
		"success":        c.Query("success"),
		"error":          c.Query("error"),
	})
}

func UpdateSettings(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(c.PostForm("base_url")), "/")
	if baseURL != "" {
		_ = model.UpdateUserBaseURL(userID, baseURL)
	}

	currentPass := c.PostForm("current_password")
	newPass := c.PostForm("new_password")
	confirmPass := c.PostForm("confirm_password")
	if newPass != "" {
		if !auth.CheckPassword(user.PasswordHash, currentPass) {
			c.Redirect(http.StatusFound, "/settings?error=Current+password+incorrect")
			return
		}
		if newPass != confirmPass || len(newPass) < 8 {
			c.Redirect(http.StatusFound, "/settings?error=New+password+must+match+and+be+8%2B+chars")
			return
		}
		hash, err := auth.HashPassword(newPass)
		if err == nil {
			_ = model.UpdateUserPassword(userID, hash)
		}
	}

	acc := parseSendingSettingsForm(c)
	existing, err := model.GetSMTPAccountByUserID(userID)
	if err == nil {
		acc.ID = existing.ID
		acc.AuthType = existing.AuthType
		acc.OAuthRefreshToken = existing.OAuthRefreshToken
		acc.OAuthAccessToken = existing.OAuthAccessToken
		acc.OAuthExpiry = existing.OAuthExpiry
		acc.GoogleEmail = existing.GoogleEmail
		acc.SMTPHost = existing.SMTPHost
		acc.SMTPPort = existing.SMTPPort
		acc.SMTPUser = existing.SMTPUser
		acc.IMAPHost = existing.IMAPHost
		acc.IMAPPort = existing.IMAPPort
		acc.IMAPUser = existing.IMAPUser
		if existing.IsGoogleOAuth() {
			acc.Status = "active"
		} else if existing.Status != "" {
			acc.Status = existing.Status
		} else {
			acc.Status = "inactive"
		}
	} else {
		acc.Status = "inactive"
	}
	if err := model.UpsertSMTPAccountForUser(userID, acc); err != nil {
		log.Print(err)
		c.Redirect(http.StatusFound, "/settings?error=Failed+to+save+settings")
		return
	}

	c.Redirect(http.StatusFound, "/settings?success=Settings+saved")
}

func parseSendingSettingsForm(c *gin.Context) model.SMTPAccount {
	daily, _ := strconv.Atoi(c.PostForm("daily_limit"))
	perMin, _ := strconv.Atoi(c.PostForm("per_minute_limit"))
	minGap, _ := strconv.Atoi(c.PostForm("min_seconds_between_sends"))
	warmCap, _ := strconv.Atoi(c.PostForm("warmup_daily_cap"))
	warmTarget, _ := strconv.Atoi(c.PostForm("warmup_target_daily_cap"))
	warmInc, _ := strconv.Atoi(c.PostForm("warmup_increment_per_day"))
	if daily == 0 {
		daily = 50
	}
	if perMin == 0 {
		perMin = 2
	}
	if minGap == 0 {
		minGap = 30
	}
	if warmCap == 0 {
		warmCap = 5
	}
	if warmTarget == 0 {
		warmTarget = daily
	}
	if warmInc == 0 {
		warmInc = 5
	}
	fromName := c.PostForm("from_name")
	return model.SMTPAccount{
		FromName:               fromName,
		DailyLimit:             daily,
		PerMinuteLimit:         perMin,
		MinSecondsBetweenSends: minGap,
		WarmupEnabled:          c.PostForm("warmup_enabled") == "on",
		WarmupDailyCap:         warmCap,
		WarmupTargetDailyCap:   warmTarget,
		WarmupIncrementPerDay:  warmInc,
	}
}
