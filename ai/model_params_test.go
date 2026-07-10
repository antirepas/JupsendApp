package ai

import "testing"

func TestIsReasoningChatModel(t *testing.T) {
	tests := map[string]bool{
		"gpt-5-nano":        true,
		"gpt-5-mini":        true,
		"gpt-5":             true,
		"o3-mini":           true,
		"gpt-5-chat-latest": false,
		"gpt-4.1-nano":      false,
		"gpt-4o-mini":       false,
	}
	for model, want := range tests {
		if got := isReasoningChatModel(model); got != want {
			t.Fatalf("isReasoningChatModel(%q) = %v want %v", model, got, want)
		}
	}
}

func TestBuildChatCompletionPayloadReasoningModel(t *testing.T) {
	payload := buildChatCompletionPayload("gpt-5-nano", "sys", "user")
	if _, ok := payload["temperature"]; ok {
		t.Fatal("reasoning model payload must not include temperature")
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Fatal("reasoning model payload must not include max_tokens")
	}
	if payload["max_completion_tokens"] != 512 {
		t.Fatalf("max_completion_tokens = %v", payload["max_completion_tokens"])
	}
	if payload["reasoning_effort"] != "minimal" {
		t.Fatalf("reasoning_effort = %v", payload["reasoning_effort"])
	}
}

func TestBuildChatCompletionPayloadLegacyModel(t *testing.T) {
	payload := buildChatCompletionPayload("gpt-4.1-nano", "sys", "user")
	if payload["temperature"] != 0.7 {
		t.Fatalf("temperature = %v", payload["temperature"])
	}
	if payload["max_tokens"] != 512 {
		t.Fatalf("max_tokens = %v", payload["max_tokens"])
	}
	if _, ok := payload["max_completion_tokens"]; ok {
		t.Fatal("legacy model payload must not include max_completion_tokens")
	}
}
