package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestCampaignABWinnerNoSends(t *testing.T) {
	db.OpenTestDB(t)

	userID, _ := CreateUser("ab-winner@test.com", "hash", "http://localhost")
	tpl := Template{Name: "Original", Subject: "Hi", Body: "<p>Test</p>"}
	templateAID, _ := tpl.SaveTemplate(userID, nil)
	tplB := Template{Name: "Variant B", Subject: "Hello", Body: "<p>B</p>"}
	templateBID, _ := tplB.SaveTemplate(userID, nil)

	campaignID, _ := CreateCampaign(userID, "Test", templateAID, templateBID, "bulk", 0, "subject", "hypothesis")

	winner, method := CampaignABWinner(campaignID)
	if winner != "" {
		t.Fatalf("expected no winner without sends, got %q (%q)", winner, method)
	}
}
