package outbound

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/util"
)

var (
	reInReplyTo         = regexp.MustCompile(`(?i)In-Reply-To:\s*<?([^>\r\n]+)`)
	reReferences        = regexp.MustCompile(`(?i)References:\s*([^\r\n]+)`)
	reReplySendIDHeader = regexp.MustCompile(`(?i)X-EmailTracker-Send-ID:\s*(\d+)`)
	reEmailTrackerMsgID = regexp.MustCompile(`@emailtracker>`)
	reAngleMsgID        = regexp.MustCompile(`<([^>\s]+)>`)
	reBareMsgID         = regexp.MustCompile(`(?i)(?:^|[\s,])([a-z0-9._+-]+@[a-z0-9.-]+)`)
)

type ReplyMatch struct {
	ContactID   int64
	EmailSendID int64
	TrackingID  string
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

	combined := replyCombinedText(body, subject, inReplyTo)
	if reEmailTrackerMsgID.MatchString(combined) {
		return true
	}
	if reReplySendIDHeader.MatchString(body) {
		return true
	}
	// Current sends use Message-ID <trackingID@senderDomain>; resolve via tracking lookup.
	if resolveSendIDFromMessageIDs(combined) > 0 {
		return true
	}
	return false
}

func replyCombinedText(body, subject string, inReplyTo []string) string {
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
	return combined
}

func ExtractSendIDFromReply(body string, inReplyTo []string) int64 {
	if id := ExtractSendIDFromBounce(body); id > 0 {
		return id
	}
	combined := replyCombinedText(body, "", inReplyTo)
	if reEmailTrackerMsgID.MatchString(combined) {
		reMsgID := regexp.MustCompile(`<([^>]+@emailtracker)>`)
		if m := reMsgID.FindStringSubmatch(combined); len(m) > 1 {
			trackID := strings.TrimSuffix(m[1], "@emailtracker")
			if id, err := model.GetEmailSendIDByTrackingID(trackID); err == nil {
				return id
			}
		}
	}
	return resolveSendIDFromMessageIDs(combined)
}

// resolveSendIDFromMessageIDs looks up outbound sends by tracking id embedded in
// Message-ID local-parts (e.g. <uuid@customerdomain.com>).
func resolveSendIDFromMessageIDs(combined string) int64 {
	if db.DB == nil {
		return 0
	}
	seen := map[string]struct{}{}
	tryTrack := func(raw string) int64 {
		raw = normalizeMessageID(raw)
		if raw == "" {
			return 0
		}
		if _, ok := seen[raw]; ok {
			return 0
		}
		seen[raw] = struct{}{}
		local := raw
		if i := strings.Index(raw, "@"); i > 0 {
			local = raw[:i]
		}
		if local == "" {
			return 0
		}
		if id, err := model.GetEmailSendIDByTrackingID(local); err == nil && id > 0 {
			return id
		}
		return 0
	}

	for _, m := range reAngleMsgID.FindAllStringSubmatch(combined, -1) {
		if len(m) > 1 {
			if id := tryTrack(m[1]); id > 0 {
				return id
			}
		}
	}
	// Envelope In-Reply-To may omit angle brackets.
	for _, m := range reBareMsgID.FindAllStringSubmatch(combined, -1) {
		if len(m) > 1 {
			if id := tryTrack(m[1]); id > 0 {
				return id
			}
		}
	}
	return 0
}

func MatchReply(userID int64, from, subject, body string, inReplyTo []string, userOwnEmail string) (ReplyMatch, bool) {
	sendID := ExtractSendIDFromReply(body, inReplyTo)
	contactID := int64(0)
	trackingID := ""

	// Prefer Message-ID resolution; also allow contact+recent-send fallback without
	// requiring legacy @emailtracker markers.
	isLikelyReply := IsReplyMessage(from, subject, body, inReplyTo, userOwnEmail)
	if !isLikelyReply && sendID == 0 {
		// Soft path: known contact From + Re: subject or In-Reply-To present.
		fromEmail := strings.Trim(strings.ToLower(from), "<>")
		own := strings.TrimSpace(strings.ToLower(userOwnEmail))
		if fromEmail == "" || fromEmail == own || IsBounceMessage(from, subject, body) {
			return ReplyMatch{}, false
		}
		hasThread := len(inReplyTo) > 0 || strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:")
		if !hasThread {
			return ReplyMatch{}, false
		}
		c, err := model.FindContactByEmail(userID, fromEmail)
		if err != nil {
			return ReplyMatch{}, false
		}
		recentID, err := model.FindRecentSendToContact(userID, c.ID, 90)
		if err != nil || recentID == 0 {
			return ReplyMatch{}, false
		}
		sendID = recentID
		contactID = c.ID
		if detail, err := model.GetEmailSendDetail(sendID); err == nil {
			trackingID = detail.TrackingID
		}
		return ReplyMatch{ContactID: contactID, EmailSendID: sendID, TrackingID: trackingID}, true
	}
	if !isLikelyReply && sendID == 0 {
		return ReplyMatch{}, false
	}

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

func handleReply(userID int64, match ReplyMatch, msg inboxMessage, accountID int64, ownEmail string) {
	dedupe := replyDedupeKey(match, msg.MessageID)
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
			"source":  "imap",
			"subject": msg.Subject,
		},
		OccurredAt: time.Now(),
	})

	parsed := util.ParseMIMEBody(msg.Body)
	toEmail := ownEmail
	if match.EmailSendID > 0 {
		if detail, err := model.GetEmailSendDetail(match.EmailSendID); err == nil && detail.SenderEmail != "" {
			toEmail = detail.SenderEmail
		}
	}
	_, _ = model.InsertConversationMessage(model.ConversationMessageInput{
		UserID:        userID,
		ContactID:     match.ContactID,
		SMTPAccountID: accountID,
		EmailSendID:   match.EmailSendID,
		Direction:     model.ConversationInbound,
		FromEmail:     msg.From,
		ToEmail:       toEmail,
		Subject:       msg.Subject,
		BodyText:      parsed.Text,
		BodyHTML:      parsed.HTML,
		MessageID:     msg.MessageID,
		InReplyTo:     msg.InReplyTo,
		OccurredAt:    time.Now(),
	})

	_ = model.MarkContactReplied(match.ContactID)
	campaignID := int64(0)
	if match.EmailSendID > 0 {
		if detail, err := model.GetEmailSendDetail(match.EmailSendID); err == nil {
			campaignID = detail.CampaignID
		}
	}
	model.ApplyStopOnReplyForContact(match.ContactID, campaignID)
	if campaignID > 0 {
		model.MaybeStopWorkflowOnHot(campaignID, match.ContactID)
	}
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
