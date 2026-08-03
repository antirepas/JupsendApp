package model

import (
	"testing"
	"time"
)

func TestPageContactEngagementRows(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	rows := []ContactEngagementRow{
		{ContactID: 1, Email: "b@test.com", OpenCount: 1, ClickCount: 0, Engaged: true, SentAt: &earlier},
		{ContactID: 2, Email: "a@test.com", OpenCount: 5, ClickCount: 2, Engaged: true, SentAt: &now},
		{ContactID: 3, Email: "c@test.com", OpenCount: 0, ClickCount: 0, Engaged: false},
	}

	paged, meta := PageContactEngagementRows(rows, AnalyticsContactFilter{Sort: "opens", Page: 1, PageSize: 2})
	if meta.Total != 3 || meta.TotalPages != 2 {
		t.Fatalf("meta=%+v", meta)
	}
	if len(paged) != 2 || paged[0].Email != "a@test.com" {
		t.Fatalf("opens sort paged=%+v", paged)
	}

	filtered, meta2 := PageContactEngagementRows(rows, AnalyticsContactFilter{Query: "a@", Sort: "email", Page: 1, PageSize: 50})
	if meta2.Total != 1 || filtered[0].Email != "a@test.com" {
		t.Fatalf("query filter=%+v meta=%+v", filtered, meta2)
	}
}

func TestPageWorkflowContactRows(t *testing.T) {
	rows := []CampaignWorkflowContactAnalytics{
		{ContactID: 1, Email: "z@test.com", OpenCount: 1, HasReplied: false, InstanceStatus: "active"},
		{ContactID: 2, Email: "a@test.com", OpenCount: 0, HasReplied: true, InstanceStatus: "completed"},
	}
	paged, meta := PageWorkflowContactRows(rows, AnalyticsContactFilter{Sort: "replied", Page: 1, PageSize: 50})
	if meta.Total != 2 || paged[0].Email != "a@test.com" {
		t.Fatalf("replied sort=%+v meta=%+v", paged, meta)
	}
}
