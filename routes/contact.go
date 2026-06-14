package routes

import (
	"log"
	"net/http"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

type SC struct {
	CS  []model.Contact          `json:"contacts"`
	CVS []model.ContactVariables `json:"contact_variables"`
}

func SaveContacts(ctx *gin.Context) {
	var sc SC
	err := ctx.ShouldBindJSON(&sc)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "could not get request body"})
		return
	}

	for _, C := range sc.CS {
		var cvs []model.ContactVariables
		for _, V := range sc.CVS {
			if C.ID == V.ContactID {
				cvs = append(cvs, V)
			}
		}
		_, err = C.SaveContact(cvs)
		if err != nil {
			log.Print(err)
			continue
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "contacts saved successfully!"})
}
