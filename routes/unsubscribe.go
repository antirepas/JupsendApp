package routes

import (
	"net/http"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func UnsubscribePage(c *gin.Context) {
	token := c.Param("token")
	userID, contactID, err := model.VerifyUnsubscribeToken(token)
	if err != nil {
		c.HTML(http.StatusBadRequest, "unsubscribe_confirm.html", gin.H{
			"title": "Unsubscribe",
			"error": "This unsubscribe link is invalid or has expired.",
		})
		return
	}
	ownerID, err := model.GetUserIDForContact(contactID)
	if err != nil || ownerID != userID {
		c.HTML(http.StatusBadRequest, "unsubscribe_confirm.html", gin.H{
			"title": "Unsubscribe",
			"error": "This unsubscribe link is invalid or has expired.",
		})
		return
	}
	_ = model.SuppressContact(contactID, "unsubscribe", "link", 0)
	c.HTML(http.StatusOK, "unsubscribe_confirm.html", gin.H{
		"title":   "Unsubscribed",
		"success": true,
	})
}
