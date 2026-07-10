package routes

import (
	"log"
	"net/http"
	"net/url"
	"strconv"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func WorkflowArchivePreviewJSON(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow"})
		return
	}
	preview, err := model.GetWorkflowArchivePreview(id, mustUserID(ctx))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	ctx.JSON(http.StatusOK, preview)
}

func ArchiveWorkflowWeb(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/workflows?error=Invalid+workflow")
		return
	}
	cancelQueued := ctx.PostForm("cancel_queued") == "1"
	if err := model.ArchiveWorkflow(id, mustUserID(ctx), cancelQueued); err != nil {
		log.Print(err)
		redirect := "/workflows?error=" + url.QueryEscape(err.Error())
		if ctx.GetHeader("Referer") != "" {
			redirect = ctx.GetHeader("Referer")
			if u, parseErr := url.Parse(redirect); parseErr == nil {
				q := u.Query()
				q.Set("error", err.Error())
				u.RawQuery = q.Encode()
				redirect = u.String()
			}
		}
		ctx.Redirect(http.StatusFound, redirect)
		return
	}
	msg := "Workflow archived"
	if cancelQueued {
		msg = "Workflow archived and queued emails cancelled"
	}
	ctx.Redirect(http.StatusFound, "/workflows?success="+url.QueryEscape(msg))
}
