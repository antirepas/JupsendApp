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

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"/promote-winner", nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(campaignID, 10)}}
	setTestUser(ctx, userID)

	// No sends yet — no winner
	PromoteCampaignWinner(ctx)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "error=No") {
		t.Fatalf("location=%s", w.Header().Get("Location"))
	}
}
