package routes

import (
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/notify"
)

func wireProvisionNotifications() {
	model.NotifyProvisionQueuedFn = func(userID int64, kind, domain string, mailboxEmails []string) {
		email, label := userNotifyMeta(userID)
		notify.NotifyProvisionQueued(email, label, kind, domain, mailboxEmails)
	}
	model.NotifyProvisionReadyFn = func(userID int64, domain string) {
		email, label := userNotifyMeta(userID)
		notify.NotifyProvisionReady(email, label, domain)
	}
}

func userNotifyMeta(userID int64) (email, label string) {
	u, err := model.GetUserByID(userID)
	if err != nil {
		return "", ""
	}
	email = strings.TrimSpace(u.Email)
	label = email
	if at := strings.IndexByte(label, '@'); at > 0 {
		label = label[:at]
	}
	return email, label
}
