package model

import "testing"

func TestPickABWinnerOpenFirst(t *testing.T) {
	a := VariantAnalytics{Sent: 50, UniqueOpens: 15, OpenRate: 30, ClickRate: 5}
	b := VariantAnalytics{Sent: 50, UniqueOpens: 10, OpenRate: 20, ClickRate: 2}
	winner, method := pickABWinner(a, b, true)
	if winner != "A" || method != "open" {
		t.Fatalf("got %q %q", winner, method)
	}
}

func TestPickABWinnerFallbackOpens(t *testing.T) {
	a := VariantAnalytics{Sent: 5, OpenRate: 25, ClickRate: 3}
	b := VariantAnalytics{Sent: 5, OpenRate: 15, ClickRate: 2}
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
