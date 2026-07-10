package routes

import (
	"log"
	"net/http"
	"net/url"
	"strconv"

	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func DownloadContactSample(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var vars []string
	idStr := ctx.Param("id")
	if idStr != "" && idStr != "generic" {
		templateID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			ctx.String(http.StatusBadRequest, "Invalid template ID")
			return
		}
		_, templateVars, err := model.GetTemplateByID(templateID, userID)
		if err != nil {
			ctx.String(http.StatusNotFound, "Template not found")
			return
		}
		vars = templateVars
	}

	data, err := util.CreateContactSampleExcel(vars)
	if err != nil {
		log.Print(err)
		ctx.String(http.StatusInternalServerError, "Could not create sample file")
		return
	}

	ctx.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	ctx.Header("Content-Disposition", "attachment; filename=contacts_sample.xlsx")
	ctx.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func PreviewContactsUpload(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var vars []string
	if templateIDStr := ctx.PostForm("template_id"); templateIDStr != "" {
		templateID, err := strconv.ParseInt(templateIDStr, 10, 64)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template"})
			return
		}
		_, templateVars, err := model.GetTemplateByID(templateID, userID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Template not found"})
			return
		}
		vars = templateVars
	}

	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	src, err := file.Open()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Could not read uploaded file"})
		return
	}
	defer src.Close()

	peek, err := util.PeekContactsUpload(src, file.Filename, vars)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, peek)
}

func UploadContacts(ctx *gin.Context) {
	userID := mustUserID(ctx)
	var vars []string
	if templateIDStr := ctx.PostForm("template_id"); templateIDStr != "" {
		templateID, err := strconv.ParseInt(templateIDStr, 10, 64)
		if err != nil {
			ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Invalid+template+selected")
			return
		}
		_, templateVars, err := model.GetTemplateByID(templateID, userID)
		if err != nil {
			ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Template+not+found")
			return
		}
		vars = templateVars
	}

	listID, _ := strconv.ParseInt(ctx.PostForm("list_id"), 10, 64)

	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=No+file+uploaded")
		return
	}

	src, err := file.Open()
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Could+not+read+uploaded+file")
		return
	}
	defer src.Close()

	rows, err := util.ParseContactsUpload(src, file.Filename, vars)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error="+url.QueryEscape(err.Error()))
		return
	}

	importKeys := vars
	if len(importKeys) == 0 && len(rows) > 0 {
		importKeys = keysFromImportRows(rows)
	}

	result, err := model.ImportContactRows(userID, parseImportRowsFromExcel(rows, vars), listID, importKeys)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error="+url.QueryEscape(err.Error()))
		return
	}

	msg := model.FormatImportResultMessage(result)
	ctx.Redirect(http.StatusFound, "/contacts?tab=import&success="+url.QueryEscape(msg))
}

func keysFromImportRows(rows []util.ContactImportRow) []string {
	seen := map[string]bool{}
	var keys []string
	for _, row := range rows {
		for k := range row.Variables {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func keysFromImportRowsParsed(rows []model.ImportContactRow) []string {
	seen := map[string]bool{}
	var keys []string
	for _, row := range rows {
		for k := range row.Variables {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func ValidateContactsWeb(ctx *gin.Context) {
	userID := mustUserID(ctx)
	contacts, err := model.ListContacts(userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=all&error=Validation+failed")
		return
	}
	validated := 0
	invalid := 0
	for _, ctt := range contacts {
		ok, reason := util.ValidateEmail(ctt.Email)
		if ok {
			_ = model.SetContactEmailStatus(ctt.ID, "valid", "")
			validated++
		} else {
			_ = model.SetContactEmailStatus(ctt.ID, "invalid", reason)
			invalid++
		}
	}
	msg := strconv.Itoa(validated) + " valid, " + strconv.Itoa(invalid) + " invalid"
	ctx.Redirect(http.StatusFound, "/contacts?tab=all&success="+url.QueryEscape(msg))
}
