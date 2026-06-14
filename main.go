package main

import (
	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/routes"
	"github.com/gin-gonic/gin"
)

func main() {
	config.Load()

	server := gin.Default()

	db.Prepare()
	defer db.DB.Close()

	routes.RegisterRoutes(server)

	server.Run(":" + config.Port)
}
