package util

import "testing"

func TestComputeGoalProgress(t *testing.T) {
	p := ComputeGoalProgress(OutreachGoals{
		MeetingsPerMonth:  10,
		ReplyToMeetingPct: 50,
		DailySendCap:      30,
	}, 8)

	if !p.HasGoals {
		t.Fatal("expected goals")
	}
	if p.RepliesNeeded != 20 {
		t.Fatalf("replies needed=%v want 20", p.RepliesNeeded)
	}
	if p.RepliesProgressPct != 40 {
		t.Fatalf("progress=%v want 40", p.RepliesProgressPct)
	}
	if p.RequiredReplyRate <= 0 {
		t.Fatal("expected required reply rate")
	}
}

func TestComputeGoalProgressNoGoals(t *testing.T) {
	p := ComputeGoalProgress(OutreachGoals{}, 5)
	if p.HasGoals {
		t.Fatal("expected no goals")
	}
}
