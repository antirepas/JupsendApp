package model

import "testing"

func TestPickABWinnerReplyFirst(t *testing.T) {
	a := VariantAnalytics{Sent: 50, UniqueReplies: 5, ReplyRate: 10, OpenRate: 20, ClickRate: 2}
	b := VariantAnalytics{Sent: 50, UniqueReplies: 2, ReplyRate: 4, OpenRate: 30, ClickRate: 5}
	winner, method := pickABWinner(a, b, true)
	if winner != "A" || method != "reply" {
		t.Fatalf("got %q %q", winner, method)
	}
}

func TestPickABWinnerFallbackOpens(t *testing.T) {
	a := VariantAnalytics{Sent: 50, OpenRate: 25, ClickRate: 3}
	b := VariantAnalytics{Sent: 50, OpenRate: 15, ClickRate: 2}
	winner, method := pickABWinner(a, b, true)
	if winner != "A" || method != "opens" {
		t.Fatalf("got %q %q", winner, method)
	}
}

func TestPickABWinnerNoVariantB(t *testing.T) {
	winner, method := pickABWinner(VariantAnalytics{}, VariantAnalytics{}, false)
	if winner != "" || method != "" {
		t.Fatalf("got %q %q", winner, method)
	}
}
