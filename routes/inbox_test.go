package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestInterestedContactsRedirectToInbox(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/contacts/interested", InterestedContactsRedirect)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/contacts/interested", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/inbox?folder=interested" {
		t.Fatalf("location=%q", loc)
	}
}

func TestSendsPageRedirectToInbox(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/sends", SendsPageRedirect)
	router.GET("/sends/new", NewSendRedirect)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sends", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/inbox?folder=sent" {
		t.Fatalf("sends -> %d %q", w.Code, w.Header().Get("Location"))
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/sends/new?contact_id=9", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusFound || w2.Header().Get("Location") != "/inbox/compose?contact_id=9" {
		t.Fatalf("sends/new -> %d %q", w2.Code, w2.Header().Get("Location"))
	}
}

func TestInboxMarkReadEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)
	userID, _ := model.CreateUser(fmt.Sprintf("inbox-route-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	c := model.Contact{Email: "lead@example.com"}
	cid, _ := c.SaveContact(userID, nil)
	_, err := model.InsertConversationMessage(model.ConversationMessageInput{
		UserID: userID, ContactID: cid, Direction: model.ConversationInbound,
		FromEmail: "lead@example.com", ToEmail: "me@test.com", Subject: "Re: hi",
		BodyText: "Hello", ReplySentiment: model.ReplySentimentPositive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.CountInboxUnread(userID) != 1 {
		t.Fatal("expected unread before mark")
	}

	router := gin.New()
	router.POST("/inbox/threads/:contactId/read", func(ctx *gin.Context) {
		setTestUser(ctx, userID)
		InboxMarkRead(ctx)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/inbox/threads/%d/read", cid), nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status=%d", w.Code)
	}
	if model.CountInboxUnread(userID) != 0 {
		t.Fatalf("unread=%d after mark", model.CountInboxUnread(userID))
	}
}
