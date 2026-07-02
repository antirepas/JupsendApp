package routes

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestPromoteCampaignWinner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	userID, _ := model.CreateUser("promote@test.com", "hash", "http://localhost")
	tpl := model.Template{Name: "Original", Subject: "Hi", Body: "<p>Test</p>"}
	templateAID, _ := tpl.SaveTemplate(userID, nil)
	tplB := model.Template{Name: "Variant B", Subject: "Hello", Body: "<p>B</p>"}
	templateBID, _ := tplB.SaveTemplate(userID, nil)

	campaignID, _ := model.CreateCampaign(userID, "Test", templateAID, templateBID, "bulk", 0, "subject", "hypothesis")

	router := gin.New()
	router.POST("/campaigns/:id/promote-winner", func(c *gin.Context) {
		setTestUser(c, userID)
		PromoteCampaignWinner(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"/promote-winner", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d body=%q", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "error=No") {
		t.Fatalf("location=%q", location)
	}
}
