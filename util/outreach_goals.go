package util

type OutreachGoals struct {
	MeetingsPerMonth    int
	ReplyToMeetingPct   int
	DailySendCap        int
}

type GoalProgress struct {
	RepliesNeeded       float64
	RequiredReplyRate   float64
	RepliesThisMonth    int
	RepliesProgressPct  float64
	HasGoals            bool
}

func ComputeGoalProgress(goals OutreachGoals, repliesThisMonth int) GoalProgress {
	p := GoalProgress{RepliesThisMonth: repliesThisMonth}
	if goals.MeetingsPerMonth <= 0 {
		return p
	}
	p.HasGoals = true
	pct := goals.ReplyToMeetingPct
	if pct <= 0 {
		pct = 50
	}
	p.RepliesNeeded = float64(goals.MeetingsPerMonth) / (float64(pct) / 100)
	if p.RepliesNeeded > 0 {
		p.RepliesProgressPct = float64(repliesThisMonth) / p.RepliesNeeded * 100
	}
	if goals.DailySendCap > 0 {
		sendsPerMonth := float64(goals.DailySendCap * 22)
		if sendsPerMonth > 0 {
			p.RequiredReplyRate = p.RepliesNeeded / sendsPerMonth * 100
		}
	}
	return p
}
