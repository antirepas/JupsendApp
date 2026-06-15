package routes

import (
	"log"
	"net/http"
	"strconv"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func ListWorkflowsPage(ctx *gin.Context) {
	workflows, err := model.ListWorkflows(mustUserID(ctx))
	if err != nil {
		log.Print(err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "active": "workflows", "error": "Failed to load workflows"})
		return
	}
	ctx.HTML(http.StatusOK, "workflows_list.html", gin.H{
		"title":     "Workflows",
		"active":    "workflows",
		"workflows": workflows,
		"success":   ctx.Query("success"),
		"error":     ctx.Query("error"),
	})
}

func NewWorkflowPage(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "workflows_form.html", gin.H{
		"title":  "New Workflow",
		"active": "workflows",
	})
}

func CreateWorkflowWeb(ctx *gin.Context) {
	name := ctx.PostForm("name")
	desc := ctx.PostForm("description")
	id, err := model.CreateWorkflow(mustUserID(ctx), name, desc)
	if err != nil {
		ctx.Redirect(http.StatusFound, "/workflows/new?error=Failed+to+create")
		return
	}
	ctx.Redirect(http.StatusFound, "/workflows/"+strconv.FormatInt(id, 10)+"/edit")
}

func WorkflowBuilderPage(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "workflows", "error": "Invalid ID"})
		return
	}
	w, err := model.GetWorkflowForUser(id, mustUserID(ctx))
	if err != nil {
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "workflows", "error": "Not found"})
		return
	}
	templates, _ := model.ListTemplates(mustUserID(ctx))
	ctx.HTML(http.StatusOK, "workflows_builder.html", gin.H{
		"title":     w.Name,
		"active":    "workflows",
		"workflow":  w,
		"versionID": w.CurrentVersionID,
		"templates": templates,
	})
}

func WorkflowAnalyticsPage(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{"title": "Error", "active": "workflows", "error": "Invalid ID"})
		return
	}
	w, err := model.GetWorkflowForUser(id, mustUserID(ctx))
	if err != nil {
		ctx.HTML(http.StatusNotFound, "error.html", gin.H{"title": "Error", "active": "workflows", "error": "Not found"})
		return
	}
	stats, _ := model.CountInstancesByStatus(id)
	graph, _ := model.GetWorkflowGraph(w.CurrentVersionID)
	nodeStats := []model.WorkflowNodeStat{}
	for _, n := range graph.Nodes {
		active, _ := model.CountInstancesAtNode(w.CurrentVersionID, n.NodeKey)
		nodeStats = append(nodeStats, model.WorkflowNodeStat{
			NodeKey:  n.NodeKey,
			Label:    n.Label,
			NodeType: n.NodeType,
			Active:   active,
		})
	}
	ctx.HTML(http.StatusOK, "workflows_analytics.html", gin.H{
		"title":     w.Name + " Analytics",
		"active":    "workflows",
		"workflow":  w,
		"stats":     stats,
		"nodeStats": nodeStats,
	})
}
