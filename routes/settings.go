package routes

import (
	"log"
	"net/http"
	"net/url"
	"strconv"

	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"emailtracker.com/util"
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
	mailboxReady := false
	if acc.ID > 0 {
		mailboxReady = acc.IsSendReady()
	}
	goalsPreview := util.ComputeGoalProgress(util.OutreachGoals{
		MeetingsPerMonth:  user.GoalMeetingsPerMonth,
		ReplyToMeetingPct: user.GoalReplyToMeetingPct,
		DailySendCap:      user.GoalDailySendCap,
	}, 0)
	c.HTML(http.StatusOK, "settings.html", gin.H{
		"title":           "Settings",
		"active":          "settings",
		"user":            user,
		"account":         acc,
		"mailboxReady":    mailboxReady,
		"goalsPreview":    goalsPreview,
		"gmailConnected":  acc.IsGoogleOAuth(),
		"gmailConfigured": googleoauth.IsConfigured(),
		"gmailError":      model.GmailSendBlocked(userID),
		"subscribed":      model.UserHasAppAccess(user),
		"isPro":           model.UserIsPro(userID),
		"success":         c.Query("success"),
		"error":           c.Query("error"),
	})
}

func UpdateSettings(c *gin.Context) {
	userID := mustUserID(c)
	if _, err := model.GetUserByID(userID); err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	existing, err := model.GetSMTPAccountByUserID(userID)
	if err == nil && existing.ID > 0 {
		existing.FromName = c.PostForm("from_name")
		if existing.IsGoogleOAuth() {
			existing.Status = "active"
		}
		if err := model.UpsertSMTPAccountForUser(userID, existing); err != nil {
			log.Print(err)
			c.Redirect(http.StatusFound, "/settings?error=Failed+to+save+settings")
			return
		}
	}

	cooldown, _ := strconv.Atoi(c.PostForm("send_cooldown_days"))
	_ = model.UpdateUserSendCooldownDays(userID, cooldown)
	_ = model.UpdateUserIncludeUnsubscribeLink(userID, c.PostForm("include_unsubscribe_link") == "on")

	meetings, _ := strconv.Atoi(c.PostForm("goal_meetings_per_month"))
	replyPct, _ := strconv.Atoi(c.PostForm("goal_reply_to_meeting_pct"))
	dailyCap, _ := strconv.Atoi(c.PostForm("goal_daily_send_cap"))
	_ = model.UpdateUserOutreachGoals(userID, meetings, replyPct, dailyCap)

	c.Redirect(http.StatusFound, "/settings?success=Settings+saved")
}

func SettingsSMTPCheck(c *gin.Context) {
	from, err := runUserSMTPCheck(mustUserID(c), 0)
	if err != nil {
		c.Redirect(http.StatusFound, "/settings?error="+url.QueryEscape("SMTP test failed: "+err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/settings?success="+url.QueryEscape("SMTP OK for "+from+" — ready to send."))
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
