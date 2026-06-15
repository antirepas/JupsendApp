package util

import (
	"fmt"
	"regexp"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/model"
)

func RenderTemplate(tBody string, contactVars []model.ContactVariables, trackId string) (string, error) {
	config.Reload()

	varMap := make(map[string]string)
	for _, cv := range contactVars {
		varMap[cv.Key] = cv.Value
	}

	for key, value := range varMap {
		placeholder := "{{" + key + "}}"
		tBody = strings.ReplaceAll(tBody, placeholder, value)
	}

	return tBody, nil
}

// InjectTrackingPixel appends a 1x1 open-tracking image inside the HTML body.
func InjectTrackingPixel(body, trackId string) string {
	base := config.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	pixelURL := fmt.Sprintf("%s/api/v1/track/open/%s", base, trackId)

	pixel := fmt.Sprintf(
		`<img src="%s" width="1" height="1" alt="" border="0" style="display:block;width:1px;height:1px;border:0;margin:0;padding:0;line-height:1;" />`,
		pixelURL,
	)
	// CSS background fallback for clients that block hidden <img> tags
	pixel += fmt.Sprintf(
		`<div aria-hidden="true" style="width:1px;height:1px;max-height:0;overflow:hidden;line-height:1px;background-image:url('%s');background-repeat:no-repeat;"></div>`,
		pixelURL,
	)
	// Table-based fallback for clients that ignore hidden images
	pixel += fmt.Sprintf(
		`<div style="display:none;max-height:0;overflow:hidden;"><img src="%s" width="1" height="1" alt="" /></div>`,
		pixelURL,
	)

	lower := strings.ToLower(body)
	if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
		return body[:idx] + pixel + body[idx:]
	}
	return body + pixel
}

func RewriteLinks(htmlBody string, emailSendId int64) string {
	config.Reload()
	re := regexp.MustCompile(`href="([^"]+)"`)

	return re.ReplaceAllStringFunc(htmlBody, func(match string) string {
		originalURL := re.FindStringSubmatch(match)[1]

		linkTrackingID := fmt.Sprintf("%d", GenerateID())
		_, err := model.SaveTrackLink(emailSendId, linkTrackingID, originalURL)
		if err != nil {
			return match
		}

		base := config.BaseURL
		if base == "" {
			base = "http://localhost:8080"
		}
		trackingURL := fmt.Sprintf(`href="%s/api/v1/track/click/%s"`, base, linkTrackingID)
		return trackingURL
	})
}
