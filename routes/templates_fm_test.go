package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestTemplateOpsMoveCopyRename(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	email := fmt.Sprintf("fm-route-%d@test.com", time.Now().UnixNano())
	userID, err := model.CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := model.CreateTemplateFolder(userID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	tpl := model.Template{Name: "Hello", Subject: "Hi", Body: "<p>x</p>"}
	tid, err := tpl.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	withUser := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			setTestUser(c, userID)
			h(c)
		}
	}
	router.POST("/templates/ops/move", withUser(TemplateOpsMove))
	router.POST("/templates/ops/copy", withUser(TemplateOpsCopy))
	router.POST("/templates/ops/rename", withUser(TemplateOpsRename))

	fid := folderID
	body, _ := json.Marshal(map[string]interface{}{"ids": []int64{tid}, "folder_id": fid})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/templates/ops/move", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("move code=%d body=%s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]interface{}{"ids": []int64{tid}, "folder_id": 0})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/templates/ops/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("copy code=%d body=%s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]interface{}{"kind": "template", "id": tid, "name": "Hello 2"})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/templates/ops/rename", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rename code=%d body=%s", w.Code, w.Body.String())
	}
	got, err := model.GetTemplate(tid)
	if err != nil || got.Name != "Hello 2" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
