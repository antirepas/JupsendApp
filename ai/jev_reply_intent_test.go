package ai

import (
	"strings"
	"testing"
)

func TestBuildReplyIntentState(t *testing.T) {
	state := BuildReplyIntentState("Re: hello", "Yes let's book a call next week")
	reply, _ := state["reply"].(map[string]interface{})
	if !strings.Contains(reply["body"].(string), "book a call") {
		t.Fatalf("body=%v", reply["body"])
	}
}

func TestNormalizeIntentAndSentiment(t *testing.T) {
	if normalizeIntent("MEETING_ASK") != "meeting_ask" {
		t.Fatal("normalize")
	}
	if intentToSentiment("pricing") != "positive" {
		t.Fatal("pricing sentiment")
	}
	if intentToSentiment("unsubscribe") != "negative" {
		t.Fatal("unsub sentiment")
	}
	if intentToSentiment("ooo") != "neutral" {
		t.Fatal("ooo sentiment")
	}
}

func TestBuildReplyVerifyState(t *testing.T) {
	state := BuildReplyVerifyState("hi", "What's your price?", "Re: hi", "Our packages start at…", "pricing")
	in, _ := state["inbound"].(map[string]interface{})
	if in["intent"] != "pricing" {
		t.Fatalf("%v", in)
	}
}

func TestFormatReplyVerifySkipReason(t *testing.T) {
	vr := ReplyVerifyResult{
		Decision:              "skip",
		Confidence:            0.82,
		PrimaryGap:            "does_not_answer",
		AnswersInbound:        0.2,
		GrammaticallyOK:       0.9,
		AppropriateCommitment: 0.7,
	}
	got := FormatReplyVerifySkipReason(vr, "interested")
	if !strings.Contains(got, "interested") || !strings.Contains(got, "does not address") {
		t.Fatalf("reason=%q", got)
	}
	if FormatReplyVerifySkipReason(ReplyVerifyResult{Decision: "send"}, "interested") != "" {
		t.Fatal("send should have empty reason")
	}
}
