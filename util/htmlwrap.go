package util

import "strings"

func WrapHTMLBody(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<html") {
		return body
	}
	return `<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body>` + body + `</body></html>`
}
