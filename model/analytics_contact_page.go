package model

import (
	"math"
	"sort"
	"strings"
	"time"
)

// AnalyticsContactFilter pages/sorts per-contact analytics tables.
type AnalyticsContactFilter struct {
	Query    string
	Sort     string // email, opens, clicks, sent, engaged, emails_sent, status, replied
	Page     int
	PageSize int
}

// AnalyticsContactPage is pagination metadata for analytics contact tables.
type AnalyticsContactPage struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	Sort       string
	Query      string
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
}

func normalizeAnalyticsContactFilter(f AnalyticsContactFilter) AnalyticsContactFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	if f.Sort == "" {
		f.Sort = "email"
	}
	return f
}

func buildAnalyticsContactPage(total int, f AnalyticsContactFilter) AnalyticsContactPage {
	f = normalizeAnalyticsContactFilter(f)
	totalPages := int(math.Ceil(float64(total) / float64(f.PageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	if f.Page > totalPages {
		f.Page = totalPages
	}
	return AnalyticsContactPage{
		Page:       f.Page,
		PageSize:   f.PageSize,
		Total:      total,
		TotalPages: totalPages,
		Sort:       f.Sort,
		Query:      f.Query,
		HasPrev:    f.Page > 1,
		HasNext:    f.Page < totalPages,
		PrevPage:   f.Page - 1,
		NextPage:   f.Page + 1,
	}
}

func pageSlice[T any](items []T, f AnalyticsContactFilter) ([]T, AnalyticsContactPage) {
	f = normalizeAnalyticsContactFilter(f)
	meta := buildAnalyticsContactPage(len(items), f)
	if len(items) == 0 {
		return items, meta
	}
	start := (meta.Page - 1) * meta.PageSize
	if start >= len(items) {
		start = 0
		meta.Page = 1
	}
	end := start + meta.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], meta
}

// PageContactEngagementRows filters, sorts, and pages bulk analytics contact rows.
func PageContactEngagementRows(rows []ContactEngagementRow, f AnalyticsContactFilter) ([]ContactEngagementRow, AnalyticsContactPage) {
	f = normalizeAnalyticsContactFilter(f)
	q := strings.ToLower(strings.TrimSpace(f.Query))
	filtered := make([]ContactEngagementRow, 0, len(rows))
	for _, r := range rows {
		if q != "" && !strings.Contains(strings.ToLower(r.Email), q) {
			continue
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		switch f.Sort {
		case "opens":
			if a.OpenCount != b.OpenCount {
				return a.OpenCount > b.OpenCount
			}
		case "clicks":
			if a.ClickCount != b.ClickCount {
				return a.ClickCount > b.ClickCount
			}
		case "sent":
			return timePtrAfter(a.SentAt, b.SentAt)
		case "engaged":
			if a.Engaged != b.Engaged {
				return a.Engaged && !b.Engaged
			}
			if a.OpenCount != b.OpenCount {
				return a.OpenCount > b.OpenCount
			}
		}
		return strings.ToLower(a.Email) < strings.ToLower(b.Email)
	})
	return pageSlice(filtered, f)
}

// PageWorkflowContactRows filters, sorts, and pages workflow analytics contact rows.
func PageWorkflowContactRows(rows []CampaignWorkflowContactAnalytics, f AnalyticsContactFilter) ([]CampaignWorkflowContactAnalytics, AnalyticsContactPage) {
	f = normalizeAnalyticsContactFilter(f)
	q := strings.ToLower(strings.TrimSpace(f.Query))
	filtered := make([]CampaignWorkflowContactAnalytics, 0, len(rows))
	for _, r := range rows {
		if q != "" && !strings.Contains(strings.ToLower(r.Email), q) {
			continue
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		switch f.Sort {
		case "opens":
			if a.OpenCount != b.OpenCount {
				return a.OpenCount > b.OpenCount
			}
		case "clicks":
			if a.ClickCount != b.ClickCount {
				return a.ClickCount > b.ClickCount
			}
		case "emails_sent":
			if a.EmailsSent != b.EmailsSent {
				return a.EmailsSent > b.EmailsSent
			}
		case "status":
			if a.InstanceStatus != b.InstanceStatus {
				return a.InstanceStatus < b.InstanceStatus
			}
		case "replied":
			if a.HasReplied != b.HasReplied {
				return a.HasReplied && !b.HasReplied
			}
		}
		return strings.ToLower(a.Email) < strings.ToLower(b.Email)
	})
	return pageSlice(filtered, f)
}

func timePtrAfter(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return a.After(*b)
}
