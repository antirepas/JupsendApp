package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func TestCreateTemplateFolderRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db.OpenTestDB(t)

	email := fmt.Sprintf("folder-route-%d@test.com", time.Now().UnixNano())
	userID, err := model.CreateUser(email, "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/templates/folders", func(c *gin.Context) {
		setTestUser(c, userID)
		CreateTemplateFolder(c)
	})

	form := url.Values{}
	form.Set("name", "Sales")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/templates/folders", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "success=") || !strings.Contains(loc, "folder=") {
		t.Fatalf("location=%q", loc)
	}
	folders, err := model.ListTemplateFolders(userID)
	if err != nil || len(folders) != 1 || folders[0].Name != "Sales" {
		t.Fatalf("folders=%+v err=%v", folders, err)
	}

	// List page filter smoke
	router.GET("/templates", func(c *gin.Context) {
		setTestUser(c, userID)
		ListTemplatesPage(c)
	})
	// HTML rendering needs templates loaded — skip full HTML; exercise model path via redirect target.
	fid := folders[0].ID
	items, err := model.ListTemplatesFiltered(userID, strconv.FormatInt(fid, 10))
	if err != nil || len(items) != 0 {
		t.Fatalf("empty folder list err=%v n=%d", err, len(items))
	}
}
