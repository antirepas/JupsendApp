package outbound

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/model"
)

var (
	reInReplyTo       = regexp.MustCompile(`(?i)In-Reply-To:\s*<?([^>\r\n]+)`)
	reReferences      = regexp.MustCompile(`(?i)References:\s*([^\r\n]+)`)
	reReplySendIDHeader = regexp.MustCompile(`(?i)X-EmailTracker-Send-ID:\s*(\d+)`)
	reEmailTrackerMsgID = regexp.MustCompile(`@emailtracker>`)
)

type ReplyMatch struct {
	ContactID  int64
	EmailSendID int64
	TrackingID string
}

func IsReplyMessage(from, subject, body string, inReplyTo []string, userOwnEmail string) bool {
	if IsBounceMessage(from, subject, body) {
		return false
	}
	from = strings.TrimSpace(strings.ToLower(from))
	if from == "" || reMailerDaemon.MatchString(from) {
		return false
	}
	own := strings.TrimSpace(strings.ToLower(userOwnEmail))
	if own != "" && from == own {
		return false
	}

	combined := body + " " + subject
	for _, ref := range inReplyTo {
		combined += " " + ref
	}
	if m := reInReplyTo.FindStringSubmatch(body); len(m) > 1 {
		combined += " " + m[1]
	}
	if m := reReferences.FindStringSubmatch(body); len(m) > 1 {
		combined += " " + m[1]
	}
	if reEmailTrackerMsgID.MatchString(combined) {
		return true
	}
	if reReplySendIDHeader.MatchString(body) {
		return true
	}
	return false
}

func ExtractSendIDFromReply(body string, inReplyTo []string) int64 {
	if id := ExtractSendIDFromBounce(body); id > 0 {
		return id
	}
	combined := body
	for _, ref := range inReplyTo {
		combined += " " + ref
	}
	if m := reInReplyTo.FindStringSubmatch(body); len(m) > 1 {
		combined += " " + m[1]
	}
	if m := reReferences.FindStringSubmatch(body); len(m) > 1 {
		combined += " " + m[1]
	}
	// tracking id in Message-ID: <uuid@emailtracker>
	if reEmailTrackerMsgID.MatchString(combined) {
		// try to resolve via tracking id embedded in angle brackets
		reMsgID := regexp.MustCompile(`<([^>]+@emailtracker)>`)
		if m := reMsgID.FindStringSubmatch(combined); len(m) > 1 {
			trackID := strings.TrimSuffix(m[1], "@emailtracker")
			if id, err := model.GetEmailSendIDByTrackingID(trackID); err == nil {
				return id
			}
		}
	}
	return 0
}

func MatchReply(userID int64, from, subject, body string, inReplyTo []string, userOwnEmail string) (ReplyMatch, bool) {
	if !IsReplyMessage(from, subject, body, inReplyTo, userOwnEmail) {
		return ReplyMatch{}, false
	}

	sendID := ExtractSendIDFromReply(body, inReplyTo)
	contactID := int64(0)
	trackingID := ""

	if sendID > 0 {
		detail, err := model.GetEmailSendDetail(sendID)
		if err == nil {
			contactID = detail.ContactID
			trackingID = detail.TrackingID
		}
	}

	if contactID == 0 {
		fromEmail := strings.Trim(strings.ToLower(from), "<>")
		if c, err := model.FindContactByEmail(userID, fromEmail); err == nil {
			contactID = c.ID
			if sendID == 0 {
				sendID, _ = model.FindRecentSendToContact(userID, contactID, 90)
			}
			if sendID > 0 && trackingID == "" {
				if detail, err := model.GetEmailSendDetail(sendID); err == nil {
					trackingID = detail.TrackingID
				}
			}
		}
	}

	if contactID == 0 {
		return ReplyMatch{}, false
	}

	// Require recent send relationship when we only matched by from address
	if sendID == 0 {
		recentID, err := model.FindRecentSendToContact(userID, contactID, 90)
		if err != nil || recentID == 0 {
			return ReplyMatch{}, false
		}
		sendID = recentID
		if detail, err := model.GetEmailSendDetail(sendID); err == nil {
			trackingID = detail.TrackingID
		}
	}

	return ReplyMatch{
		ContactID:   contactID,
		EmailSendID: sendID,
		TrackingID:  trackingID,
	}, true
}

func handleReply(userID int64, match ReplyMatch, imapMessageID string) {
	dedupe := replyDedupeKey(match, imapMessageID)
	if dedupe != "" {
		if exists, _ := model.ContactEventExistsByDedupe(dedupe); exists {
			return
		}
	} else if match.EmailSendID > 0 {
		if exists, _ := model.HasContactEventForSend(match.EmailSendID, "REPLY"); exists {
			return
		}
	}

	var wfID int64
	if match.EmailSendID > 0 {
		instID := model.GetSendWorkflowInstanceID(match.EmailSendID)
		if instID > 0 {
			inst, err := model.GetWorkflowInstance(instID)
			if err == nil {
				v, _ := model.GetWorkflowVersion(inst.WorkflowVersionID)
				wfID = v.WorkflowID
			}
		}
	}
	_, _ = model.InsertContactEvent(model.ContactEventInput{
		ContactID:   match.ContactID,
		WorkflowID:  wfID,
		EmailSendID: match.EmailSendID,
		EventType:   "REPLY",
		DedupeKey:   dedupe,
		Metadata: map[string]interface{}{
			"source": "imap",
		},
		OccurredAt: time.Now(),
	})
	_ = model.MarkContactReplied(match.ContactID)
	_ = model.CancelActiveInstancesForContact(match.ContactID)
}

func replyDedupeKey(match ReplyMatch, imapMessageID string) string {
	if mid := normalizeMessageID(imapMessageID); mid != "" {
		return "imap-msg:" + mid
	}
	if match.EmailSendID > 0 {
		return "reply:" + strconv.FormatInt(match.EmailSendID, 10)
	}
	return ""
}

func normalizeMessageID(id string) string {
	return strings.Trim(strings.TrimSpace(id), "<>")
}
