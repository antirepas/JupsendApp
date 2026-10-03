package model

import (
	"fmt"
	"sort"
	"strings"

	"emailtracker.com/db"
)

const EmailVerifyChunkSize = 200

// EmailVerificationResult is one Apify (or stub) verification row.
type EmailVerificationResult struct {
	Email      string
	Result     string
	Subresult  string
	Quality    string
	Role       bool
	Free       bool
	DidYouMean string
	Error      string
}

// EmailVerifyFunc verifies a batch of emails. Injected by routes (Apify client) or tests.
type EmailVerifyFunc func(emails []string) ([]EmailVerificationResult, error)

// VerifySummary is returned after a verify run.
type VerifySummary struct {
	Submitted        int
	Valid            int
	Invalid          int
	Risky            int
	SkippedNoCredits int
	Errors           int
	CreditsRemaining int
	CreditsCap       int
}

func (s VerifySummary) FlashMessage() string {
	return fmt.Sprintf(
		"Verified %d · %d valid · %d invalid · %d risky · %d skipped (credits)%s",
		s.Submitted, s.Valid, s.Invalid, s.Risky, s.SkippedNoCredits,
		func() string {
			if s.Errors > 0 {
				return fmt.Sprintf(" · %d update errors", s.Errors)
			}
			return ""
		}(),
	)
}

// MapEmailVerificationResult maps Apify result → email_status + reason.
func MapEmailVerificationResult(r EmailVerificationResult) (status, reason string) {
	result := strings.ToLower(strings.TrimSpace(r.Result))
	switch result {
	case "ok":
		status = "valid"
	case "invalid", "disposable":
		status = "invalid"
	default:
		// catch_all, unknown, error, empty
		status = "risky"
	}

	parts := []string{}
	if result != "" {
		parts = append(parts, "result="+result)
	}
	if s := strings.TrimSpace(r.Subresult); s != "" {
		parts = append(parts, "subresult="+s)
	}
	if q := strings.TrimSpace(r.Quality); q != "" {
		parts = append(parts, "quality="+q)
	}
	if r.Role {
		parts = append(parts, "role")
	}
	if r.Free {
		parts = append(parts, "free")
	}
	if d := strings.TrimSpace(r.DidYouMean); d != "" {
		parts = append(parts, "didyoumean="+d)
	}
	if e := strings.TrimSpace(r.Error); e != "" {
		parts = append(parts, "error="+e)
	}
	reason = strings.Join(parts, "; ")
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return status, reason
}

// VerifyContactsForUser verifies contact emails via verifyFn, charges 1 AI credit per submitted email.
// onlyUnverified skips contacts already marked valid/invalid/risky.
func VerifyContactsForUser(userID int64, contactIDs []int64, onlyUnverified bool, verifyFn EmailVerifyFunc) (VerifySummary, error) {
	var sum VerifySummary
	if verifyFn == nil {
		return sum, fmt.Errorf("email verification is not configured (set APIFY_TOKEN)")
	}
	if len(contactIDs) == 0 {
		cap, rem, _ := AICreditsRemaining(userID)
		sum.CreditsCap, sum.CreditsRemaining = cap, rem
		return sum, nil
	}

	type row struct {
		id    int64
		email string
	}
	var work []row
	for _, id := range contactIDs {
		if id <= 0 {
			continue
		}
		var email, status string
		var owner int64
		err := dbQueryContactEmailStatus(id, &owner, &email, &status)
		if err != nil || owner != userID {
			continue
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		status = strings.ToLower(strings.TrimSpace(status))
		if onlyUnverified && (status == "valid" || status == "invalid" || status == "risky") {
			continue
		}
		work = append(work, row{id: id, email: email})
	}
	if len(work) == 0 {
		cap, rem, _ := AICreditsRemaining(userID)
		sum.CreditsCap, sum.CreditsRemaining = cap, rem
		return sum, nil
	}

	cap, remaining, _ := AICreditsRemaining(userID)
	sum.CreditsCap = cap
	if remaining <= 0 {
		sum.SkippedNoCredits = len(work)
		sum.CreditsRemaining = 0
		return sum, fmt.Errorf("no AI credits remaining today (cap %d)", cap)
	}
	if len(work) > remaining {
		sum.SkippedNoCredits = len(work) - remaining
		work = work[:remaining]
	}

	idByEmail := map[string][]int64{}
	for _, w := range work {
		idByEmail[w.email] = append(idByEmail[w.email], w.id)
	}
	emails := make([]string, 0, len(idByEmail))
	for e := range idByEmail {
		emails = append(emails, e)
	}
	sort.Strings(emails)

	for i := 0; i < len(emails); i += EmailVerifyChunkSize {
		end := i + EmailVerifyChunkSize
		if end > len(emails) {
			end = len(emails)
		}
		chunk := emails[i:end]
		results, err := verifyFn(chunk)
		if err != nil {
			return sum, fmt.Errorf("verification provider: %w", err)
		}
		// Charge for emails submitted in this chunk (1 credit each), even if provider omitted a row.
		if _, rem, ok := ConsumeAICredits(userID, len(chunk)); !ok {
			sum.CreditsRemaining = rem
			return sum, fmt.Errorf("could not consume AI credits for verification")
		} else {
			sum.CreditsRemaining = rem
		}
		sum.Submitted += len(chunk)

		got := map[string]EmailVerificationResult{}
		for _, r := range results {
			key := strings.ToLower(strings.TrimSpace(r.Email))
			if key != "" {
				got[key] = r
			}
		}
		for _, email := range chunk {
			r, ok := got[email]
			if !ok {
				r = EmailVerificationResult{Email: email, Result: "unknown", Subresult: "missing_from_provider"}
			}
			status, reason := MapEmailVerificationResult(r)
			switch status {
			case "valid":
				sum.Valid++
			case "invalid":
				sum.Invalid++
			default:
				sum.Risky++
			}
			for _, cid := range idByEmail[email] {
				if err := SetContactEmailStatus(cid, status, reason); err != nil {
					sum.Errors++
				}
			}
		}
	}
	return sum, nil
}

func dbQueryContactEmailStatus(contactID int64, userID *int64, email, status *string) error {
	return db.QueryRow(`
		SELECT user_id, COALESCE(email, ''), COALESCE(email_status, 'unknown')
		FROM contact WHERE id = ?
	`, contactID).Scan(userID, email, status)
}

// ListContactIDsInList returns contact IDs for a list owned by userID.
func ListContactIDsInList(listID, userID int64) ([]int64, error) {
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT m.contact_id
		FROM contact_list_members m
		INNER JOIN contact c ON c.id = m.contact_id
		WHERE m.list_id = ? AND c.user_id = ?
		ORDER BY m.contact_id ASC
	`, listID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
