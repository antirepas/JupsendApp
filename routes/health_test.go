package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"emailtracker.com/db"
	"github.com/gin-gonic/gin"
)

func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	r := gin.New()
	r.GET("/healthz", Healthz)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}
