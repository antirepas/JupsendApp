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
	templateID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.String(http.StatusBadRequest, "Invalid template ID")
		return
	}

	_, vars, err := model.GetTemplateByID(templateID, mustUserID(ctx))
	if err != nil {
		ctx.String(http.StatusNotFound, "Template not found")
		return
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

func UploadContacts(ctx *gin.Context) {
	userID := mustUserID(ctx)
	templateID, err := strconv.ParseInt(ctx.PostForm("template_id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Invalid+template+selected")
		return
	}

	_, vars, err := model.GetTemplateByID(templateID, userID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Template+not+found")
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

	rows, err := util.ParseContactsExcel(src, vars)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error="+url.QueryEscape(err.Error()))
		return
	}

	result, err := model.ImportContactRows(userID, parseImportRowsFromExcel(rows, vars), listID)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?tab=import&error=Import+failed")
		return
	}

	msg := model.FormatImportResultMessage(result)
	ctx.Redirect(http.StatusFound, "/contacts?tab=import&success="+url.QueryEscape(msg))
}
