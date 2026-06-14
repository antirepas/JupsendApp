package util

import (
	"fmt"
	"regexp"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/model"
)

func RenderTemplate(tBody string, contactVars []model.ContactVariables, trackId string) (string, error) {
	varMap := make(map[string]string)

	for _, cv := range contactVars {
		varMap[cv.Key] = cv.Value
	}

	for key, value := range varMap {
		placeholder := "{{" + key + "}}"
		tBody = strings.ReplaceAll(tBody, placeholder, value)
	}

	if trackId != "" {
		pixel := fmt.Sprintf(
			`<img src="%s/api/v1/track/open/%s" width="1" height="1" style="display:none;" />`,
			config.BaseURL,
			trackId,
		)
		tBody += pixel
	}
	return tBody, nil
}

func RewriteLinks(htmlBody string, emailSendId int64) string {
	re := regexp.MustCompile(`href="([^"]+)"`)

	return re.ReplaceAllStringFunc(htmlBody, func(match string) string {
		originalURL := re.FindStringSubmatch(match)[1]

		linkTrackingID := fmt.Sprintf("%d", GenerateID())
		_, err := model.SaveTrackLink(emailSendId, linkTrackingID, originalURL)
		if err != nil {
			return match
		}

		trackingURL := fmt.Sprintf(
			`href="%s/api/v1/track/click/%s"`,
			config.BaseURL,
			linkTrackingID,
		)
		return trackingURL
	})
}
