package ai

import (
	"testing"

	"emailtracker.com/config"
)

func TestTransformModelReasoningPrimary(t *testing.T) {
	t.Setenv("OPENAI_MODEL", "gpt-5-nano")
	t.Setenv("OPENAI_FALLBACK_MODEL", "")
	config.Reload()
	if got := transformModel(); got != "gpt-4.1-mini" {
		t.Fatalf("transformModel() = %q want gpt-4.1-mini", got)
	}
}

func TestTransformModelChatPrimary(t *testing.T) {
	t.Setenv("OPENAI_MODEL", "gpt-4.1-mini")
	config.Reload()
	if got := transformModel(); got != "gpt-4.1-mini" {
		t.Fatalf("transformModel() = %q", got)
	}
}

func TestTransformModelCustomFallback(t *testing.T) {
	t.Setenv("OPENAI_MODEL", "gpt-5-nano")
	t.Setenv("OPENAI_FALLBACK_MODEL", "gpt-4o-mini")
	config.Reload()
	if got := transformModel(); got != "gpt-4o-mini" {
		t.Fatalf("transformModel() = %q want gpt-4o-mini", got)
	}
}
