package routes

import (
	"net/http"
	"strconv"
	"strings"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

type templateOpsBody struct {
	IDs      []int64 `json:"ids"`
	FolderID *int64  `json:"folder_id"` // nil ignored; 0 = unfiled
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"` // template | folder
}

func parseFolderDest(body templateOpsBody) (int64, bool) {
	if body.FolderID == nil {
		return 0, false
	}
	return *body.FolderID, true
}

func TemplateOpsMove(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body templateOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "ids required"})
		return
	}
	folderID, ok := parseFolderDest(body)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "folder_id required"})
		return
	}
	n, err := model.MoveTemplatesToFolder(userID, body.IDs, folderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

func TemplateOpsCopy(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body templateOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "ids required"})
		return
	}
	folderID, ok := parseFolderDest(body)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "folder_id required"})
		return
	}
	n, err := model.CopyTemplatesToFolder(userID, body.IDs, folderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

func TemplateOpsRename(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body templateOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || body.ID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id required"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	var err error
	switch kind {
	case "folder":
		err = model.RenameTemplateFolder(body.ID, userID, body.Name)
	case "template", "":
		err = model.RenameTemplate(body.ID, userID, body.Name)
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid kind"})
		return
	}
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "name": strings.TrimSpace(body.Name)})
}

func TemplateOpsDelete(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body templateOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	switch kind {
	case "folder":
		if body.ID <= 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id required"})
			return
		}
		if err := model.DeleteTemplateFolder(body.ID, userID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "could not delete folder"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": 1})
	case "template", "":
		ids := body.IDs
		if body.ID > 0 {
			ids = append(ids, body.ID)
		}
		if len(ids) == 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "ids required"})
			return
		}
		n, err := model.DeleteTemplates(userID, ids)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid kind"})
	}
}

func TemplateOpsCreateFolder(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body templateOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	id, err := model.CreateTemplateFolder(userID, body.Name)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "name": strings.TrimSpace(body.Name)})
}

// FolderDestKey converts UI folder key (all|unfiled|:id) to folder_id for paste/move.
func FolderDestKey(key string) (folderID int64, ok bool) {
	key = strings.TrimSpace(strings.ToLower(key))
	switch key {
	case "", "all", "unfiled":
		return 0, true
	default:
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 {
			return 0, false
		}
		return id, true
	}
}
