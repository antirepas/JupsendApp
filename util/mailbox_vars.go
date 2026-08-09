package util

import "strings"

// MailboxVarsFromSender builds {{@…}} values from the mailbox that will send the email.
// Supported keys: name (From display name), first_name, email.
func MailboxVarsFromSender(fromName, fromEmail string) map[string]string {
	fromName = strings.TrimSpace(fromName)
	fromEmail = strings.TrimSpace(fromEmail)
	return map[string]string{
		"name":       fromName,
		"first_name": firstWord(fromName),
		"email":      fromEmail,
	}
}

func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func lookupMailboxVar(mailboxVars map[string]string, name string) string {
	if mailboxVars == nil {
		return ""
	}
	if v, ok := mailboxVars[name]; ok {
		return v
	}
	for k, v := range mailboxVars {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}
