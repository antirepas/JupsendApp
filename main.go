package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"

	"emailtracker.com/config"
	"emailtracker.com/db"
	"emailtracker.com/model"
	"emailtracker.com/outbound"
	"emailtracker.com/routes"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func main() {
	config.Load()
	outbound.LoadConfig()

	secret := config.SessionSecret
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err == nil {
			secret = hex.EncodeToString(b)
		} else {
			secret = "dev-insecure-session-secret-change-me"
		}
		log.Println("WARNING: SESSION_SECRET not set; using ephemeral dev secret (sessions reset on restart)")
	}

	server := gin.Default()
	server.SetTrustedProxies(nil)
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 14,
		HttpOnly: true,
		SameSite: 2,
	})
	server.Use(sessions.Sessions("emailtracker_session", store))

	db.Prepare()
	defer db.Close()

	engine := routes.InitWorkflowEngine()
	routes.StartCampaignScheduler()
	routes.StartWorkflowScheduler(engine)
	model.SyncAdminEmailsFromConfig()
	outbound.StartWorker()
	outbound.StartIMAPPoller()
	routes.RegisterRoutes(server)

	server.Run(":" + config.Port)
}
