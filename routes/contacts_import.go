package routes

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

func templateVarsFromForm(ctx *gin.Context, userID int64) ([]string, error) {
	templateIDStr := ctx.PostForm("template_id")
	if templateIDStr == "" {
		return nil, nil
	}
	templateID, err := strconv.ParseInt(templateIDStr, 10, 64)
	if err != nil {
		return nil, err
	}
	_, templateVars, err := model.GetTemplateByID(templateID, userID)
	if err != nil {
		return nil, err
	}
	return templateVars, nil
}

func parseColumnMapForm(ctx *gin.Context) map[string]string {
	raw := strings.TrimSpace(ctx.PostForm("column_map"))
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func PreviewContactsUpload(ctx *gin.Context) {
	userID := mustUserID(ctx)
	vars, err := templateVarsFromForm(ctx, userID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template"})
		return
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

	peek, err := util.PeekContactsUploadRaw(src, file.Filename, vars)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, peek)
}

func PreviewContactsPaste(ctx *gin.Context) {
	userID := mustUserID(ctx)
	vars, err := templateVarsFromForm(ctx, userID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template"})
		return
	}
	paste := ctx.PostForm("paste")
	peek, ok, err := util.PeekContactPasteRaw(paste, vars)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		ctx.JSON(http.StatusOK, gin.H{"headered": false})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"headered":      true,
		"headers":       peek.Headers,
		"sample_rows":   peek.SampleRows,
		"row_count":     peek.RowCount,
		"suggested_map": peek.SuggestedMap,
		"template_vars": peek.TemplateVars,
	})
}

func UploadContacts(ctx *gin.Context) {
	userID := mustUserID(ctx)
	vars, err := templateVarsFromForm(ctx, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Invalid+template+selected")
		return
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

	colMap := parseColumnMapForm(ctx)
	var rows []util.ContactImportRow
	if len(colMap) > 0 {
		rows, err = util.ParseContactsUploadWithMap(src, file.Filename, colMap)
	} else {
		rows, err = util.ParseContactsUpload(src, file.Filename, vars)
	}
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error="+url.QueryEscape(err.Error()))
		return
	}

	importKeys := vars
	if len(importKeys) == 0 && len(rows) > 0 {
		importKeys = keysFromImportRows(rows)
	}

	result, err := model.ImportContactRows(userID, parseImportRowsFromExcel(rows, importKeys), listID, importKeys)
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
