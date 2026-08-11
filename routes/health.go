package routes

import (
	"fmt"
	"net/http"
	"strconv"
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
	from, err := runUserSMTPCheck(mustUserID(ctx), 0)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "from": from, "version": config.BuildVersion})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "from": from, "version": config.BuildVersion})
}

// runAllUserSMTPChecks probes every send-ready mailbox for the user.
func runAllUserSMTPChecks(userID int64) (okFrom []string, failMsgs []string, err error) {
	ready, err := model.ListSendReadyAccountsForUser(userID)
	if err != nil {
		return nil, nil, err
	}
	for _, acc := range ready {
		from, probeErr := runUserSMTPCheck(userID, acc.ID)
		label := from
		if label == "" {
			label = acc.SenderEmail()
		}
		if probeErr != nil {
			failMsgs = append(failMsgs, probeErr.Error())
			continue
		}
		okFrom = append(okFrom, label)
	}
	return okFrom, failMsgs, nil
}

func summarizeSMTPChecks(okFrom, failMsgs []string) (successMsg, errorMsg string) {
	total := len(okFrom) + len(failMsgs)
	if total == 0 {
		return "", "No ready mailboxes to test — finish setup under Mailboxes"
	}
	if len(failMsgs) == 0 {
		if len(okFrom) == 1 {
			return "SMTP OK for " + okFrom[0] + " — ready to send.", ""
		}
		return fmt.Sprintf("SMTP OK for all %d mailboxes (%s)", len(okFrom), strings.Join(okFrom, ", ")), ""
	}
	parts := []string{fmt.Sprintf("%d/%d mailboxes failed SMTP", len(failMsgs), total)}
	parts = append(parts, failMsgs...)
	if len(okFrom) > 0 {
		parts = append(parts, "OK: "+strings.Join(okFrom, ", "))
	}
	return "", strings.Join(parts, " · ")
}

func runUserSMTPCheck(userID, smtpAccountID int64) (from string, err error) {
	if userID == 0 {
		return "", fmt.Errorf("not logged in")
	}
	var acc model.SMTPAccount
	if smtpAccountID > 0 {
		acc, err = model.GetSMTPAccount(smtpAccountID)
		if err != nil || acc.UserID != userID {
			return "", fmt.Errorf("mailbox not found")
		}
		_ = model.EnsureDailyCounterReset(acc.ID)
		acc, err = model.GetSMTPAccount(acc.ID)
		if err != nil {
			return "", err
		}
	} else {
		acc, err = model.GetSendReadyAccountForUser(userID)
		if err != nil {
			return "", err
		}
	}
	from = acc.SenderEmail()
	if from == "" {
		return "", fmt.Errorf("no sender email configured")
	}
	label := from
	if acc.MailboxSource != "" {
		label = fmt.Sprintf("%s (%s)", from, acc.MailboxSource)
	}
	host := acc.SMTPHost
	if host == "" {
		host = "smtp.gmail.com"
	}
	port := acc.SMTPPort
	if port == "" {
		port = "587"
	}
	port = util.NormalizeGmailSMTPPort(host, port)
	if acc.MailboxSource == "inboxkit" || acc.MailboxSource == model.MailboxSourceShared || acc.MailboxSource == model.MailboxSourceManual || (!acc.IsGoogleOAuth() && acc.SMTPPassword != "") {
		pass := acc.SMTPPassword
		if acc.MailboxSource == "inboxkit" || acc.MailboxSource == model.MailboxSourceShared || acc.MailboxSource == model.MailboxSourceManual {
			pass, err = model.DecryptSMTPPassword(acc)
			if err != nil {
				return from, fmt.Errorf("%s: %w", label, err)
			}
		} else {
			pass = config.NormalizeAppPassword(pass)
		}
		user := strings.TrimSpace(acc.SMTPUser)
		if user == "" {
			user = from
		}
		if err := util.ProbeSMTPPlain(host, port, user, pass, from); err != nil {
			return from, formatSMTPProbeError(host, port, label, err)
		}
		return from, nil
	}
	if !acc.IsGoogleOAuth() {
		return from, fmt.Errorf("no ready mailbox — finish setup under Mailboxes")
	}
	token, err := model.GmailAccessToken(acc)
	if err != nil {
		return from, fmt.Errorf("%s: %w", label, err)
	}
	if err := util.ProbeSMTPAuth(host, port, from, token); err != nil {
		return from, formatSMTPProbeError(host, port, label, err)
	}
	return from, nil
}

func formatSMTPProbeError(host, port, accountLabel string, err error) error {
	if err == nil {
		return nil
	}
	prefix := ""
	if strings.TrimSpace(accountLabel) != "" {
		prefix = "for " + accountLabel + ": "
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") {
		return fmt.Errorf("%scannot reach %s:%s (%v) — outbound SMTP ports 465/587 may be blocked on this host", prefix, host, port, err)
	}
	if strings.Contains(msg, "535") || strings.Contains(msg, "badcredentials") ||
		strings.Contains(msg, "username and password not accepted") ||
		strings.Contains(msg, "authentication failed") {
		labelLower := strings.ToLower(accountLabel)
		hostLower := strings.ToLower(host)
		if strings.Contains(labelLower, "inboxkit") {
			return fmt.Errorf("%sGoogle rejected the InboxKit password — click Refresh/Pull credentials from InboxKit, then Test SMTP again", prefix)
		}
		if strings.Contains(labelLower, "manual") && strings.Contains(hostLower, "gmail") {
			return fmt.Errorf("%sGmail rejected the password. If this is a Workspace seat from jupsend, click Pull credentials from InboxKit — do not paste the Free-plan App Password. For a personal Gmail, use a Google App Password (no spaces) and turn on IMAP", prefix)
		}
		if strings.Contains(hostLower, "gmail") || strings.Contains(hostLower, "google") {
			return fmt.Errorf("%sGmail rejected the password. Use a Google App Password (Account → Security → 2-Step Verification → App passwords), not your normal login password. Paste it without spaces. Also turn on IMAP in Gmail settings", prefix)
		}
		return fmt.Errorf("%sSMTP rejected username/password for %s:%s — for Google use an App Password; for Microsoft enable SMTP AUTH / use an app password", prefix, host, port)
	}
	return fmt.Errorf("%sSMTP auth to %s:%s failed: %w", prefix, host, port, err)
}

// MailboxesSMTPCheck probes a specific outreach mailbox (not only the default).
func MailboxesSMTPCheck(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	m, err := model.GetOutreachMailbox(id, userID)
	if err != nil || m.SMTPAccountID <= 0 {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "Mailbox has no SMTP credentials to test", ""))
		return
	}
	from, err := runUserSMTPCheck(userID, m.SMTPAccountID)
	if err != nil {
		// Auto-heal: Workspace seats often get broken when pasted as "manual". Relink from InboxKit.
		var healNotes []string
		if healErr := model.RelinkMailboxFromInboxKit(userID, id); healErr == nil {
			m2, _ := model.GetOutreachMailbox(id, userID)
			if m2.SMTPAccountID > 0 {
				if from2, err2 := runUserSMTPCheck(userID, m2.SMTPAccountID); err2 == nil {
					c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "SMTP OK for "+from2+" — refreshed from InboxKit"))
					return
				} else {
					healNotes = append(healNotes, "InboxKit refresh still failed: "+err2.Error())
				}
			}
		} else {
			healNotes = append(healNotes, "InboxKit relink: "+healErr.Error())
		}
		if healErr := model.ApplySharedSMTPCredentialsToMailbox(userID, id); healErr == nil {
			m2, _ := model.GetOutreachMailbox(id, userID)
			smtpID := m.SMTPAccountID
			if m2.SMTPAccountID > 0 {
				smtpID = m2.SMTPAccountID
			}
			if from2, err2 := runUserSMTPCheck(userID, smtpID); err2 == nil {
				c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "SMTP OK for "+from2+" — using server shared credentials"))
				return
			} else {
				healNotes = append(healNotes, "shared SMTP still failed: "+err2.Error())
			}
		}
		msg := "SMTP test failed: " + err.Error()
		if len(healNotes) > 0 {
			msg += " | " + strings.Join(healNotes, "; ")
		}
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", msg, ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "SMTP OK for "+from))
}

func MailboxesRelinkInboxKit(c *gin.Context) {
	userID := mustUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := model.RelinkMailboxFromInboxKit(userID, id); err != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", humanizeInboxKitError(err.Error()), ""))
		return
	}
	m, err := model.GetOutreachMailbox(id, userID)
	if err != nil || m.SMTPAccountID <= 0 {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "Credentials refreshed from InboxKit"))
		return
	}
	from, probeErr := runUserSMTPCheck(userID, m.SMTPAccountID)
	if probeErr != nil {
		c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "Pulled InboxKit credentials but SMTP still failed: "+probeErr.Error(), ""))
		return
	}
	c.Redirect(http.StatusFound, mailboxManageURL(id, "credentials", "", "SMTP OK for "+from+" — credentials from InboxKit"))
}
