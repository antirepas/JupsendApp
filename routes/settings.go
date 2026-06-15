package routes

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/auth"
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
		"title":   "Settings",
		"active":  "settings",
		"user":    user,
		"account": acc,
		"success": c.Query("success"),
		"error":   c.Query("error"),
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

	acc := parseSMTPSettingsForm(c)
	existing, err := model.GetSMTPAccountByUserID(userID)
	if err == nil {
		if c.PostForm("smtp_password") == "" {
			acc.SMTPPassword = existing.SMTPPassword
		}
		if c.PostForm("imap_password") == "" {
			acc.IMAPPassword = existing.IMAPPassword
		}
	}
	acc.WarmupEnabled = c.PostForm("warmup_enabled") == "on"
	acc.Status = "active"
	if err := model.UpsertSMTPAccountForUser(userID, acc); err != nil {
		log.Print(err)
		c.Redirect(http.StatusFound, "/settings?error=Failed+to+save+SMTP+settings")
		return
	}

	c.Redirect(http.StatusFound, "/settings?success=Settings+saved")
}

func parseSMTPSettingsForm(c *gin.Context) model.SMTPAccount {
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
	pass := c.PostForm("smtp_password")
	return model.SMTPAccount{
		SMTPHost:               defaultStr(c.PostForm("smtp_host"), "smtp.gmail.com"),
		SMTPPort:               defaultStr(c.PostForm("smtp_port"), "587"),
		SMTPUser:               c.PostForm("smtp_user"),
		SMTPPassword:           pass,
		FromEmail:              defaultStr(c.PostForm("from_email"), c.PostForm("smtp_user")),
		FromName:               c.PostForm("from_name"),
		IMAPHost:               defaultStr(c.PostForm("imap_host"), "imap.gmail.com"),
		IMAPPort:               defaultStr(c.PostForm("imap_port"), "993"),
		IMAPUser:               defaultStr(c.PostForm("imap_user"), c.PostForm("smtp_user")),
		IMAPPassword:           c.PostForm("imap_password"),
		DailyLimit:             daily,
		PerMinuteLimit:         perMin,
		MinSecondsBetweenSends: minGap,
		WarmupDailyCap:         warmCap,
		WarmupTargetDailyCap:   warmTarget,
		WarmupIncrementPerDay:  warmInc,
	}
}
