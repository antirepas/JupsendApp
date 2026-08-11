package outbound

import "testing"

func TestIsAutoReplyMessageHeaders(t *testing.T) {
	body := "Auto-Submitted: auto-replied\r\nPrecedence: auto_reply\r\n\r\nThanks for contacting us."
	if !IsAutoReplyMessage("team@applyall.com", "Re: quick one", body) {
		t.Fatal("expected auto-reply from headers")
	}
}

func TestIsAutoReplyMessageBodyPhrase(t *testing.T) {
	if !IsAutoReplyMessage("help@x.com", "Re: hi", "Thanks for contacting us. Our team will get back to you as soon as we can.") {
		t.Fatal("expected auto-reply from body phrase")
	}
}

func TestIsAutoReplyMessageNormalReply(t *testing.T) {
	if IsAutoReplyMessage("bob@client.com", "Re: intro", "Sounds interesting — let's chat next week.") {
		t.Fatal("human reply should not be auto-reply")
	}
}

func TestIsReplyMessageSkipsAutoReply(t *testing.T) {
	body := "Auto-Submitted: auto-replied\r\n\r\nThanks for contacting us."
	if IsReplyMessage("team@x.com", "Re: Hello", body, []string{"<abc@track>"}, "me@gmail.com") {
		t.Fatal("auto-reply should not count as reply")
	}
}
