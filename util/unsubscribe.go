package util

import (
	"fmt"
	"strings"
)

func InjectUnsubscribeFooter(htmlBody, plainBody, unsubscribeURL string) (string, string) {
	if unsubscribeURL == "" {
		return htmlBody, plainBody
	}
	htmlFooter := fmt.Sprintf(
		`<p style="margin-top:24px;font-size:12px;color:#6b7280;">If you don't want to hear from me again, <a href="%s">unsubscribe</a>.</p>`,
		unsubscribeURL,
	)
	plainFooter := fmt.Sprintf("\n\nIf you don't want to hear from me again, unsubscribe: %s", unsubscribeURL)

	html := htmlBody
	if strings.Contains(strings.ToLower(html), "</body>") {
		html = strings.Replace(html, "</body>", htmlFooter+"</body>", 1)
	} else {
		html += htmlFooter
	}
	return html, plainBody + plainFooter
}
