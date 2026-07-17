package outbound

import "testing"

func TestIsReplyMessageBySendIDHeader(t *testing.T) {
	body := "Thanks!\nX-EmailTracker-Send-ID: 42"
	if !IsReplyMessage("bob@client.com", "Re: Hello", body, nil, "me@gmail.com") {
		t.Fatal("expected reply")
	}
}

func TestIsReplyMessageSkipsBounce(t *testing.T) {
	if IsReplyMessage("mailer-daemon@google.com", "Delivery failure", "X-Failed-Recipients: x", nil, "") {
		t.Fatal("bounce should not be reply")
	}
}

func TestIsReplyMessageSkipsOwnEmail(t *testing.T) {
	if IsReplyMessage("me@gmail.com", "test", "", nil, "me@gmail.com") {
		t.Fatal("own email should not count as reply")
	}
}

func TestExtractSendIDFromReply(t *testing.T) {
	body := "X-EmailTracker-Send-ID: 123"
	if id := ExtractSendIDFromReply(body, nil); id != 123 {
		t.Fatalf("got %d", id)
	}
}

func TestExtractSendIDFromReplyHeader(t *testing.T) {
	body := "Content\nX-EmailTracker-Send-ID: 456\n"
	if id := ExtractSendIDFromReply(body, nil); id != 456 {
		t.Fatalf("got %d", id)
	}
}

func TestReplyDedupeKeyPrefersMessageID(t *testing.T) {
	key := replyDedupeKey(ReplyMatch{EmailSendID: 42}, "<abc@mail.test>")
	if key != "gmail-msg:abc@mail.test" {
		t.Fatalf("got %q", key)
	}
}

func TestReplyDedupeKeyFallsBackToSendID(t *testing.T) {
	key := replyDedupeKey(ReplyMatch{EmailSendID: 42}, "")
	if key != "reply:42" {
		t.Fatalf("got %q", key)
	}
}
