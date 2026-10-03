package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestMapEmailVerificationResult(t *testing.T) {
	cases := []struct {
		result string
		want   string
	}{
		{"ok", "valid"},
		{"OK", "valid"},
		{"invalid", "invalid"},
		{"disposable", "invalid"},
		{"catch_all", "risky"},
		{"unknown", "risky"},
		{"error", "risky"},
		{"", "risky"},
	}
	for _, tc := range cases {
		status, reason := MapEmailVerificationResult(EmailVerificationResult{
			Result:    tc.result,
			Subresult: "smtp_ok",
			Quality:   "good",
			Role:      true,
			Free:      true,
		})
		if status != tc.want {
			t.Fatalf("result=%q status=%q want %q", tc.result, status, tc.want)
		}
		if tc.result != "" && !strings.Contains(reason, "result="+strings.ToLower(tc.result)) {
			t.Fatalf("reason missing result: %q", reason)
		}
		if !strings.Contains(reason, "subresult=smtp_ok") || !strings.Contains(reason, "role") || !strings.Contains(reason, "free") {
			t.Fatalf("reason incomplete: %q", reason)
		}
	}
}

func TestVerifyContactsForUserStubbed(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("verify-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	mk := func(addr string) int64 {
		c := Contact{Email: addr}
		id, err := c.SaveContact(userID, nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	idOK := mk(fmt.Sprintf("ok-%d@example.com", time.Now().UnixNano()))
	idBad := mk(fmt.Sprintf("bad-%d@example.com", time.Now().UnixNano()))
	idRisky := mk(fmt.Sprintf("risky-%d@example.com", time.Now().UnixNano()))
	idSkip := mk(fmt.Sprintf("skip-%d@example.com", time.Now().UnixNano()))
	if err := SetContactEmailStatus(idSkip, "valid", "already"); err != nil {
		t.Fatal(err)
	}

	stub := func(emails []string) ([]EmailVerificationResult, error) {
		out := make([]EmailVerificationResult, 0, len(emails))
		for _, e := range emails {
			switch {
			case strings.HasPrefix(e, "ok-"):
				out = append(out, EmailVerificationResult{Email: e, Result: "ok", Subresult: "deliverable"})
			case strings.HasPrefix(e, "bad-"):
				out = append(out, EmailVerificationResult{Email: e, Result: "invalid", Subresult: "mailbox_not_found"})
			case strings.HasPrefix(e, "risky-"):
				out = append(out, EmailVerificationResult{Email: e, Result: "catch_all"})
			default:
				out = append(out, EmailVerificationResult{Email: e, Result: "unknown"})
			}
		}
		return out, nil
	}

	sum, err := VerifyContactsForUser(userID, []int64{idOK, idBad, idRisky, idSkip}, true, stub)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Submitted != 3 || sum.Valid != 1 || sum.Invalid != 1 || sum.Risky != 1 {
		t.Fatalf("summary=%+v", sum)
	}
	if sum.SkippedNoCredits != 0 {
		t.Fatalf("unexpected skip: %+v", sum)
	}

	st, _, _ := GetContactEmailStatus(idOK)
	if st != "valid" {
		t.Fatalf("ok status=%q", st)
	}
	st, _, _ = GetContactEmailStatus(idBad)
	if st != "invalid" {
		t.Fatalf("bad status=%q", st)
	}
	st, _, _ = GetContactEmailStatus(idRisky)
	if st != "risky" {
		t.Fatalf("risky status=%q", st)
	}
	st, reason, _ := GetContactEmailStatus(idSkip)
	if st != "valid" || reason != "already" {
		t.Fatalf("skip overwritten: status=%q reason=%q", st, reason)
	}

	_, rem, ok := AICreditsRemaining(userID)
	if !ok || rem != AICreditsCapForTier(PlanTierFree)-3 {
		t.Fatalf("credits remaining=%d ok=%v", rem, ok)
	}
}

func TestVerifyContactsForUserNoCredits(t *testing.T) {
	db.OpenTestDB(t)
	email := fmt.Sprintf("verify-nocred-%d@example.com", time.Now().UnixNano())
	userID, err := CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	cap := AICreditsCapForTier(PlanTierFree)
	if _, _, ok := ConsumeAICredits(userID, cap); !ok {
		t.Fatal("could not exhaust credits")
	}
	c := Contact{Email: fmt.Sprintf("need-%d@example.com", time.Now().UnixNano())}
	cid, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := VerifyContactsForUser(userID, []int64{cid}, true, func(emails []string) ([]EmailVerificationResult, error) {
		t.Fatal("verifyFn should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected credits error")
	}
	if sum.SkippedNoCredits != 1 {
		t.Fatalf("skipped=%d", sum.SkippedNoCredits)
	}
}
