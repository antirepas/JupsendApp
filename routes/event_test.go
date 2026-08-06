package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTrackOpenSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStore := storeEventClassified
	originalRecordEngagement := recordEngagementEventFn
	originalResolve := resolveSendIDForOpen
	originalSentAt := getEmailSendSentAt
	defer func() {
		storeEventClassified = originalStore
		recordEngagementEventFn = originalRecordEngagement
		resolveSendIDForOpen = originalResolve
		getEmailSendSentAt = originalSentAt
	}()

	resolveSendIDForOpen = func(string) int64 { return 0 }
	getEmailSendSentAt = func(int64) (time.Time, error) { return time.Time{}, nil }

	engaged := false
	recordEngagementEventFn = func(string, string, map[string]interface{}) { engaged = true }

	called := false
	storeEventClassified = func(id, eventType, userAgent, ip string, isBot bool, botReason string) error {
		called = true
		if id != "abc123" {
			t.Errorf("expected id abc123, got %s", id)
		}
		if eventType != "open" {
			t.Errorf("expected event type open, got %s", eventType)
		}
		if userAgent != "Mozilla/5.0 (Macintosh)" {
			t.Errorf("unexpected ua %q", userAgent)
		}
		if !isBot {
			t.Errorf("expected localhost open to be flagged as bot")
		}
		return nil
	}

	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.GET("/api/v1/track/open/:id", TrackOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/open/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh)")
	req.RemoteAddr = "127.0.0.1:12345"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected storeEventClassified to be called")
	}
	if engaged {
		t.Fatal("bot opens must not dispatch workflow engagement")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("expected Content-Type image/gif, got %s", w.Header().Get("Content-Type"))
	}
	if len(w.Body.Bytes()) != len(trackingPixelGIF) {
		t.Fatalf("expected gif length %d, got %d", len(trackingPixelGIF), len(w.Body.Bytes()))
	}
}

func TestTrackOpenHumanViaForwardedIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStore := storeEventClassified
	originalRecordEngagement := recordEngagementEventFn
	originalResolve := resolveSendIDForOpen
	originalSentAt := getEmailSendSentAt
	defer func() {
		storeEventClassified = originalStore
		recordEngagementEventFn = originalRecordEngagement
		resolveSendIDForOpen = originalResolve
		getEmailSendSentAt = originalSentAt
	}()

	resolveSendIDForOpen = func(string) int64 { return 0 }
	getEmailSendSentAt = func(int64) (time.Time, error) { return time.Time{}, nil }

	engaged := false
	recordEngagementEventFn = func(string, string, map[string]interface{}) { engaged = true }

	storeEventClassified = func(id, eventType, userAgent, ip string, isBot bool, botReason string) error {
		if isBot {
			t.Fatalf("expected human open, got bot reason=%s ip=%s", botReason, ip)
		}
		if ip != "203.0.113.50" {
			t.Fatalf("ip=%s", ip)
		}
		return nil
	}

	router := gin.New()
	_ = router.SetTrustedProxies([]string{"127.0.0.1"})
	router.GET("/api/v1/track/open/:id", TrackOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/open/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	req.RemoteAddr = "127.0.0.1:9999"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if !engaged {
		t.Fatal("human open should dispatch engagement")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestTrackOpenStoreEventError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStore := storeEventClassified
	originalRecordEngagement := recordEngagementEventFn
	originalResolve := resolveSendIDForOpen
	originalSentAt := getEmailSendSentAt
	defer func() {
		storeEventClassified = originalStore
		recordEngagementEventFn = originalRecordEngagement
		resolveSendIDForOpen = originalResolve
		getEmailSendSentAt = originalSentAt
	}()

	resolveSendIDForOpen = func(string) int64 { return 0 }
	getEmailSendSentAt = func(int64) (time.Time, error) { return time.Time{}, nil }
	recordEngagementEventFn = func(string, string, map[string]interface{}) {}
	storeEventClassified = func(id, eventType, userAgent, ip string, isBot bool, botReason string) error {
		return errors.New("db error")
	}

	router := gin.New()
	router.GET("/api/v1/track/open/:id", TrackOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/open/abc123", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 even on store error, got %d", w.Code)
	}
}

func TestTrackClickSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStoreEvent := storeEvent
	originalGetOriginalURL := getOriginalURL
	originalRecordEngagement := recordEngagementEventFn

	defer func() {
		storeEvent = originalStoreEvent
		getOriginalURL = originalGetOriginalURL
		recordEngagementEventFn = originalRecordEngagement
	}()

	recordEngagementEventFn = func(string, string, map[string]interface{}) {}

	getOriginalURL = func(id string) (string, error) {
		if id != "abc123" {
			t.Errorf("expected id abc123, got %s", id)
		}
		return "https://example.com", nil
	}

	clickRecorded := false
	storeEvent = func(id, eventType, userAgent, ip string) error {
		if eventType != "click" {
			t.Errorf("expected event type click, got %s", eventType)
		}
		clickRecorded = true
		return nil
	}

	router := gin.New()
	router.GET("/api/v1/track/click/:id", TrackClick)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/click/abc123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if !clickRecorded {
		t.Fatal("expected storeEvent to be called for click")
	}

	if w.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "https://example.com" {
		t.Fatalf("expected redirect to https://example.com, got %s", location)
	}
}

func TestTrackClickUnknownID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalGetOriginalURL := getOriginalURL
	defer func() {
		getOriginalURL = originalGetOriginalURL
	}()

	getOriginalURL = func(id string) (string, error) {
		return "", errors.New("not found")
	}

	router := gin.New()
	router.GET("/api/v1/track/click/:id", TrackClick)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/click/missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d location=%s", w.Code, w.Header().Get("Location"))
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no redirect, got %s", loc)
	}
}

func TestTrackClickInvalidDestination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalGetOriginalURL := getOriginalURL
	originalRecordEngagement := recordEngagementEventFn
	defer func() {
		getOriginalURL = originalGetOriginalURL
		recordEngagementEventFn = originalRecordEngagement
	}()

	recordEngagementEventFn = func(string, string, map[string]interface{}) {}
	getOriginalURL = func(id string) (string, error) {
		return "/settings", nil
	}

	router := gin.New()
	router.GET("/api/v1/track/click/:id", TrackClick)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/click/abc123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d location=%s", w.Code, w.Header().Get("Location"))
	}
}
