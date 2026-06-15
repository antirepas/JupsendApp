package routes

import "github.com/gin-gonic/gin"

func setTestUser(ctx *gin.Context, userID int64) {
	ctx.Set("userID", userID)
}
