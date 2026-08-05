package routes

import (
	"fmt"
	"net/http"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func Healthz(ctx *gin.Context) {
	if err := db.Ping(); err != nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": err.Error(), "version": config.BuildVersion})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ok", "version": config.BuildVersion})
}

func OpsQueue(ctx *gin.Context) {
	stats, err := model.GetGlobalSendJobStats()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"pending":                    stats.Pending,
		"processing":                 stats.Processing,
		"dead":                       stats.Dead,
		"failed":                     stats.Failed,
		"oldest_pending_age_seconds": stats.OldestPendingAgeSeconds,
		"worker_max_concurrent":      outbound.MaxConcurrent,
		"version":                    config.BuildVersion,
	})
}

func OpsSMTPCheck(ctx *gin.Context) {
	from, err := runUserSMTPCheck(mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "from": from, "version": config.BuildVersion})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "from": from, "version": config.BuildVersion})
}

func runUserSMTPCheck(userID int64) (from string, err error) {
	if userID == 0 {
		return "", fmt.Errorf("not logged in")
	}
	acc, err := model.GetSendReadyAccountForUser(userID)
	if err != nil {
		return "", err
	}
	from = acc.SenderEmail()
	if from == "" {
		return "", fmt.Errorf("no sender email configured")
	}
	host := acc.SMTPHost
	if host == "" {
		host = "smtp.gmail.com"
	}
	port := acc.SMTPPort
	if port == "" {
		port = "465"
	}
	if acc.MailboxSource == "inboxkit" || acc.MailboxSource == model.MailboxSourceShared || acc.MailboxSource == model.MailboxSourceManual || (!acc.IsGoogleOAuth() && acc.SMTPPassword != "") {
		pass := acc.SMTPPassword
		if acc.MailboxSource == "inboxkit" || acc.MailboxSource == model.MailboxSourceShared || acc.MailboxSource == model.MailboxSourceManual {
			pass, err = model.DecryptSMTPPassword(acc)
			if err != nil {
				return from, err
			}
		}
		if err := util.ProbeSMTPPlain(host, port, acc.SMTPUser, pass, from); err != nil {
			return from, formatSMTPProbeError(host, port, err)
		}
		return from, nil
	}
	if !acc.IsGoogleOAuth() {
		return from, fmt.Errorf("no ready mailbox — finish setup under Mailboxes")
	}
	token, err := model.GmailAccessToken(acc)
	if err != nil {
		return from, err
	}
	if err := util.ProbeSMTPAuth(host, port, from, token); err != nil {
		return from, formatSMTPProbeError(host, port, err)
	}
	return from, nil
}

func formatSMTPProbeError(host, port string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") {
		return fmt.Errorf("cannot reach %s:%s (%v) — outbound SMTP ports 465/587 may be blocked on this host", host, port, err)
	}
	if strings.Contains(msg, "535") || strings.Contains(msg, "badcredentials") ||
		strings.Contains(msg, "username and password not accepted") ||
		strings.Contains(msg, "authentication failed") {
		hostLower := strings.ToLower(host)
		if strings.Contains(hostLower, "gmail") || strings.Contains(hostLower, "google") {
			return fmt.Errorf("Gmail rejected the password. Use a Google App Password (Account → Security → 2-Step Verification → App passwords), not your normal login password. Also turn on IMAP in Gmail settings")
		}
		return fmt.Errorf("SMTP rejected username/password for %s:%s — for Google use an App Password; for Microsoft enable SMTP AUTH / use an app password", host, port)
	}
	return fmt.Errorf("SMTP auth to %s:%s failed: %w", host, port, err)
}
