package routes

import (
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func LibrarySheetPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/library?error=Invalid+sheet")
		return
	}
	list, err := model.GetContactListForUser(listID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/library?error=Sheet+not+found")
		return
	}
	schema, _ := model.GetListVariableSchema(listID, userID)
	page, err := model.ListContactsInListPage(listID, userID, model.ListMembersFilter{
		Page:     1,
		PageSize: 500,
		Sort:     "email",
	})
	if err != nil {
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "library", "error": "Failed to load sheet"})
		return
	}
	templates, _ := model.ListTemplatePickerItems(userID)
	importOpen := ctx.Query("import") == "1"
	ctx.HTML(http.StatusOK, "library_sheet.html", gin.H{
		"title":      list.Name,
		"active":     "library",
		"list":       list,
		"schema":     schema,
		"rows":       page.Items,
		"total":      page.Total,
		"templates":  templates,
		"importOpen": importOpen,
		"success":    ctx.Query("success"),
		"error":      ctx.Query("error"),
	})
}

type sheetCellBody struct {
	ContactID int64  `json:"contact_id"`
	Column    string `json:"column"` // email or variable key
	Value     string `json:"value"`
}

func LibrarySheetSaveCell(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid sheet"})
		return
	}
	if _, err := model.GetContactListForUser(listID, userID); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "sheet not found"})
		return
	}
	var body sheetCellBody
	if err := ctx.ShouldBindJSON(&body); err != nil || body.ContactID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "contact_id required"})
		return
	}
	if err := model.UpdateSheetCell(userID, listID, body.ContactID, body.Column, body.Value); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

type sheetPasteBody struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

func LibrarySheetPaste(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid sheet"})
		return
	}
	var body sheetPasteBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.Rows) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "rows required"})
		return
	}
	n, err := model.PasteSheetRows(userID, listID, body.Headers, body.Rows)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

type sheetAddRowBody struct {
	Email string `json:"email"`
}

func LibrarySheetAddRow(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid sheet"})
		return
	}
	var body sheetAddRowBody
	_ = ctx.ShouldBindJSON(&body)
	email := strings.TrimSpace(strings.ToLower(body.Email))
	id, err := model.AddSheetRow(userID, listID, email)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

type sheetDeleteRowsBody struct {
	ContactIDs []int64 `json:"contact_ids"`
}

func LibrarySheetDeleteRows(ctx *gin.Context) {
	userID := mustUserID(ctx)
	listID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid sheet"})
		return
	}
	var body sheetDeleteRowsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.ContactIDs) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "contact_ids required"})
		return
	}
	n := 0
	for _, cid := range body.ContactIDs {
		if err := model.RemoveContactFromList(listID, userID, cid); err == nil {
			n++
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}
