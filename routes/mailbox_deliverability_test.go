package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestMailboxesMailTester_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("mt-nf-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mailboxes/999999/mail-tester", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	setTestUser(ctx, userID)

	MailboxesMailTester(ctx)

	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "Mailbox+not+found") && !strings.Contains(loc, "error=") {
		t.Fatalf("expected not found redirect, got %s", loc)
	}
}

func TestMailboxesMailTester_MissingSMTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("mt-nosmtp-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	oid, err := model.UpsertOutreachMailbox(model.OutreachMailbox{
		UserID: userID, Email: "seat@example.com", Status: "ready", Platform: "GOOGLE",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mailboxes/"+fmt.Sprint(oid)+"/mail-tester", nil)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(oid)}}
	setTestUser(ctx, userID)

	MailboxesMailTester(ctx)

	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "tab=deliverability") {
		t.Fatalf("expected deliverability tab, got %s", loc)
	}
	decoded, _ := url.QueryUnescape(loc)
	if !strings.Contains(decoded, "no SMTP") && !strings.Contains(decoded, "SMTP credentials") {
		t.Fatalf("expected missing SMTP error, got %s", decoded)
	}
}

func TestMailboxesMailTester_WrongUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)
	ownerID, err := model.CreateUser(fmt.Sprintf("mt-owner-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := model.CreateUser(fmt.Sprintf("mt-other-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	smtpID, err := model.UpsertInboxKitSMTPAccount(ownerID, "seat@example.com", "smtp.gmail.com", "587", "seat@example.com", "pass-aaaa-aaaa-aaaa", "Seat", "ik-mt", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	oid, err := model.UpsertOutreachMailbox(model.OutreachMailbox{
		UserID: ownerID, SMTPAccountID: smtpID, Email: "seat@example.com",
		Status: "ready", Platform: "GOOGLE", InboxkitMailboxID: "ik-mt",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mailboxes/"+fmt.Sprint(oid)+"/mail-tester", nil)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(oid)}}
	setTestUser(ctx, otherID)

	MailboxesMailTester(ctx)

	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Fatalf("expected error redirect for wrong user, got %s", loc)
	}
}

func TestMailTesterResultsURL(t *testing.T) {
	if got := mailTesterResultsURL("jupabc"); got != "https://www.mail-tester.com/jupabc" {
		t.Fatalf("%s", got)
	}
	if mailTesterResultsURL("  ") != "" {
		t.Fatal("empty token should yield empty URL")
	}
}
