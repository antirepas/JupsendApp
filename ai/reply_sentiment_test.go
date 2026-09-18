package ai

import "testing"

func TestParseReplySentimentJSON(t *testing.T) {
	got, err := ParseReplySentimentJSON(`{"sentiment":"positive","confidence":0.9}`)
	if err != nil || got.Sentiment != "positive" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = ParseReplySentimentJSON("```json\n{\"sentiment\":\"negative\",\"confidence\":0.8}\n```")
	if err != nil || got.Sentiment != "negative" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := ParseReplySentimentJSON(`{"sentiment":"meh"}`); err == nil {
		t.Fatal("expected error")
	}
}
