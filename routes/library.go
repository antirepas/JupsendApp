package routes

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func libraryFolderQuery(ctx *gin.Context) string {
	f := strings.TrimSpace(ctx.Query("folder"))
	if f == "" {
		f = strings.TrimSpace(ctx.PostForm("folder"))
	}
	if f == "" {
		return "all"
	}
	return f
}

func libraryListRedirect(folder, success, errMsg string) string {
	q := url.Values{}
	if folder != "" && folder != "all" {
		q.Set("folder", folder)
	}
	if success != "" {
		q.Set("success", success)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	if len(q) == 0 {
		return "/library"
	}
	return "/library?" + q.Encode()
}

func ListLibraryPage(ctx *gin.Context) {
	userID := mustUserID(ctx)
	folder := libraryFolderQuery(ctx)
	items, err := model.ListLibraryItems(userID, folder)
	if err != nil {
		ctx.Redirect(http.StatusFound, libraryListRedirect("all", "", "Folder not found"))
		return
	}
	folders, _ := model.ListLibraryFolders(userID)
	allCount, unfiledCount, _ := model.CountLibraryItemsForUser(userID)
	folderTitle := "All files"
	switch folder {
	case "unfiled":
		folderTitle = "Unfiled"
	case "all", "":
		folderTitle = "All files"
	default:
		if id, err := strconv.ParseInt(folder, 10, 64); err == nil {
			if f, err := model.GetLibraryFolderForUser(id, userID); err == nil {
				folderTitle = f.Name
			}
		}
	}
	ctx.HTML(http.StatusOK, "library.html", gin.H{
		"title":         "Library",
		"active":        "library",
		"items":         items,
		"folders":       folders,
		"folder":        folder,
		"folderTitle":   folderTitle,
		"allCount":      allCount,
		"unfiledCount":  unfiledCount,
		"showFolderCol": folder == "all",
		"success":       ctx.Query("success"),
		"error":         ctx.Query("error"),
	})
}

func RedirectTemplatesToLibrary(ctx *gin.Context) {
	ctx.Redirect(http.StatusFound, "/library")
}

func RedirectWorkflowsToLibrary(ctx *gin.Context) {
	ctx.Redirect(http.StatusFound, "/library")
}

func RedirectContactsToLibrary(ctx *gin.Context) {
	tab := ctx.DefaultQuery("tab", "all")
	// Keep the import hub reachable — Library replaced the contacts index.
	if tab == "import" {
		ListContactsPage(ctx)
		return
	}
	if tab == "suppressions" {
		ctx.Redirect(http.StatusFound, "/contacts/suppressions")
		return
	}
	ctx.Redirect(http.StatusFound, "/library")
}

// contactImportRedirectBase returns a path that already contains "?" for enqueueContactImport.
func contactImportRedirectBase(ctx *gin.Context) string {
	ret := strings.TrimSpace(ctx.PostForm("return_to"))
	if ret != "" && strings.HasPrefix(ret, "/") && !strings.HasPrefix(ret, "//") {
		if strings.Contains(ret, "?") {
			return ret
		}
		return ret + "?"
	}
	listID, _ := strconv.ParseInt(ctx.PostForm("list_id"), 10, 64)
	if listID > 0 {
		return "/library/sheets/" + strconv.FormatInt(listID, 10) + "?"
	}
	return "/contacts?tab=import"
}

type libraryOpsBody struct {
	Items    []model.LibraryRef `json:"items"`
	FolderID *int64             `json:"folder_id"`
	ID       int64              `json:"id"`
	Name     string             `json:"name"`
	Kind     string             `json:"kind"`
}

func LibraryOpsMove(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.Items) == 0 || body.FolderID == nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "items and folder_id required"})
		return
	}
	n, err := model.MoveLibraryItems(userID, body.Items, *body.FolderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

func LibraryOpsCopy(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || len(body.Items) == 0 || body.FolderID == nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "items and folder_id required"})
		return
	}
	n, err := model.CopyLibraryItems(userID, body.Items, *body.FolderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

func LibraryOpsRename(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil || body.ID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id required"})
		return
	}
	if err := model.RenameLibraryItem(userID, body.Kind, body.ID, body.Name); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "name": strings.TrimSpace(body.Name)})
}

func LibraryOpsDelete(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	if kind == "folder" {
		if body.ID <= 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id required"})
			return
		}
		if err := model.DeleteLibraryFolder(body.ID, userID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "could not delete folder"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": 1})
		return
	}
	if len(body.Items) == 0 && body.ID > 0 {
		body.Items = []model.LibraryRef{{Kind: kind, ID: body.ID}}
	}
	if len(body.Items) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "items required"})
		return
	}
	n, err := model.DeleteLibraryItems(userID, body.Items)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": n})
}

func LibraryOpsCreateFolder(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	id, err := model.CreateLibraryFolder(userID, body.Name)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "name": strings.TrimSpace(body.Name)})
}

func LibraryOpsCreateSheet(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	folderID := int64(0)
	if body.FolderID != nil {
		folderID = *body.FolderID
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Contacts"
	}
	id, err := model.CreateLibraryContactSheet(userID, name, folderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

func LibraryOpsCreateWorkflow(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var body libraryOpsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body"})
		return
	}
	folderID := int64(0)
	if body.FolderID != nil {
		folderID = *body.FolderID
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Workflow"
	}
	id, err := model.CreateLibraryWorkflow(userID, name, folderID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}
