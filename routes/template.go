package routes

import (
	"net/http"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

type ST struct {
	T  model.Template           `json:"template"`
	TV []model.TemplateVariable `json:"template_variables"`
}

func SaveTemplate(ctx *gin.Context) {
	var st ST
	err := ctx.ShouldBindJSON(&st)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "could not get request body"})
		return
	}
	_, err = st.T.SaveTemplate(mustUserID(ctx), st.TV)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "template saved successfully!"})
}
