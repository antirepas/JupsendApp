package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTrackOpenSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStoreEvent := storeEvent
	originalRecordEngagement := recordEngagementEventFn
	defer func() {
		storeEvent = originalStoreEvent
		recordEngagementEventFn = originalRecordEngagement
	}()

	recordEngagementEventFn = func(string, string, map[string]interface{}) {}

	called := false

	storeEvent = func(id, eventType, userAgent, ip string) error {
		called = true

		if id != "abc123" {
			t.Errorf("expected id abc123, got %s", id)
		}

		if eventType != "open" {
			t.Errorf("expected event type open, got %s", eventType)
		}

		if userAgent != "test-agent" {
			t.Errorf("expected user agent test-agent, got %s", userAgent)
		}

		return nil
	}

	router := gin.New()
	router.GET("/api/v1/track/open/:id", TrackOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/open/abc123", nil)
	req.Header.Set("User-Agent", "test-agent")

	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected storeEvent to be called")
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("expected Content-Type image/gif, got %s", w.Header().Get("Content-Type"))
	}

	if len(w.Body.Bytes()) == 0 {
		t.Fatal("expected gif body")
	}

	if len(w.Body.Bytes()) != len(trackingPixelGIF) {
		t.Fatalf("expected gif length %d, got %d", len(trackingPixelGIF), len(w.Body.Bytes()))
	}
}

func TestTrackOpenStoreEventError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalStoreEvent := storeEvent
	originalRecordEngagement := recordEngagementEventFn
	defer func() {
		storeEvent = originalStoreEvent
		recordEngagementEventFn = originalRecordEngagement
	}()

	recordEngagementEventFn = func(string, string, map[string]interface{}) {}

	storeEvent = func(id, eventType, userAgent, ip string) error {
		return errors.New("db error")
	}

	router := gin.New()
	router.GET("/api/v1/track/open/:id", TrackOpen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/track/open/abc123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 even on store error, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("expected Content-Type image/gif, got %s", w.Header().Get("Content-Type"))
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
