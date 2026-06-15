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
	server.SetTrustedProxies(nil)

	db.Prepare()
	defer db.DB.Close()

	engine := routes.InitWorkflowEngine()
	routes.StartCampaignScheduler()
	routes.StartWorkflowScheduler(engine)
	routes.RegisterRoutes(server)

	server.Run(":" + config.Port)
}
