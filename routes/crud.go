package routes

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/model"
	"emailtracker.com/util"
	"github.com/gin-gonic/gin"
)

func DeleteTemplate(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/templates?error=Invalid+template")
		return
	}
	if err := model.DeleteTemplate(id); err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/templates?error=Failed+to+delete")
		return
	}
	ctx.Redirect(http.StatusFound, "/templates?success=Template+deleted")
}

func DeleteContact(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	if err := model.DeleteContact(id); err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/contacts?error=Failed+to+delete")
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts?success=Contact+deleted")
}

func EditContactPage(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Invalid contact ID"})
		return
	}
	c, vars, err := model.GetContact(id)
	if err != nil {
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "contacts", "error": "Contact not found"})
		return
	}
	ctx.HTML(http.StatusOK, "contacts_form.html", gin.H{
		"title":   "Edit Contact",
		"active":  "contacts",
		"isEdit":  true,
		"contact": c,
		"vars":    vars,
		"error":   ctx.Query("error"),
	})
}

func UpdateContact(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	email := ctx.PostForm("email")
	keys := ctx.PostFormArray("var_key")
	values := ctx.PostFormArray("var_value")

	var cvs []model.ContactVariables
	for i, key := range keys {
		if key == "" {
			continue
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		cvs = append(cvs, model.ContactVariables{Key: key, Value: value})
	}

	if err := model.UpdateContact(id, email, cvs); err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(id, 10)+"/edit?error=Failed+to+update")
		return
	}
	ctx.Redirect(http.StatusFound, "/contacts?success=Contact+updated")
}

func DeleteCampaign(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	if err := model.DeleteCampaign(id); err != nil {
		log.Print(err)
		ctx.Redirect(http.StatusFound, "/campaigns?error=Failed+to+delete")
		return
	}
	ctx.Redirect(http.StatusFound, "/campaigns?success=Campaign+deleted")
}

func UpdateBaseURL(ctx *gin.Context) {
	baseURL := strings.TrimRight(strings.TrimSpace(ctx.PostForm("base_url")), "/")
	if baseURL == "" {
		ctx.Redirect(http.StatusFound, "/?error=BASE_URL+required")
		return
	}
	os.Setenv("BASE_URL", baseURL)
	config.Reload()
	ctx.Redirect(http.StatusFound, "/?success=Tracking+URL+updated+for+new+sends")
}

func TestTrackingPixel(ctx *gin.Context) {
	config.Reload()
	testID := fmt.Sprintf("test-%d", util.GenerateID())
	pixelURL := fmt.Sprintf("%s/api/v1/track/open/%s", config.BaseURL, testID)
	ctx.Redirect(http.StatusFound, pixelURL)
}
