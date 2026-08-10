package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestRecommendedOutreachGraphValidates(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("playbook-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	id, err := CreateRecommendedOutreachWorkflow(userID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := GetWorkflow(id)
	if err != nil {
		t.Fatal(err)
	}
	errs := ValidateWorkflowGraph(w.CurrentVersionID)
	if len(errs) > 0 {
		t.Fatalf("validation errors: %v", errs)
	}
	g, err := GetWorkflowGraph(w.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	temps := 0
	for _, n := range g.Nodes {
		switch n.NodeType {
		case "action_send_email":
			sends++
		case "condition_temperature":
			temps++
		}
	}
	if sends != 4 {
		t.Fatalf("expected 4 send steps, got %d", sends)
	}
	if temps != 2 {
		t.Fatalf("expected 2 temperature nodes, got %d", temps)
	}
}
