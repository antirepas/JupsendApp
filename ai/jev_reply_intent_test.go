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

