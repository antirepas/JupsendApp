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

func TestLibraryOpsAndRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	email := fmt.Sprintf("lib-route-%d@test.com", time.Now().UnixNano())
	userID, err := model.CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := model.CreateLibraryFolder(userID, "Pack")
	if err != nil {
		t.Fatal(err)
	}
	tpl := model.Template{Name: "Hi", Subject: "S", Body: "B"}
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
	router.GET("/templates", withUser(RedirectTemplatesToLibrary))
	router.POST("/library/ops/move", withUser(LibraryOpsMove))
	router.POST("/library/ops/rename", withUser(LibraryOpsRename))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/library" {
		t.Fatalf("redirect code=%d loc=%q", w.Code, w.Header().Get("Location"))
	}

	fid := folderID
	body, _ := json.Marshal(map[string]interface{}{
		"items":     []map[string]interface{}{{"kind": "template", "id": tid}},
		"folder_id": fid,
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/library/ops/move", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("move code=%d body=%s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]interface{}{"kind": "template", "id": tid, "name": "Hello"})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/library/ops/rename", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rename code=%d body=%s", w.Code, w.Body.String())
	}
}
