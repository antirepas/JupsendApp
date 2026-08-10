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

	if config.TokenEncryptionKey == "" && config.SessionSecret == "" {
		log.Println("WARNING: TOKEN_ENCRYPTION_KEY not set — Gmail OAuth tokens use a dev-only key; set the same key on production and reconnect Gmail")
	}
	log.Printf("emailtracker starting (version=%s)", config.BuildVersion)
	if config.IsLocalDev() {
		log.Printf("WARNING: dev login enabled at /auth/dev/login (email=%s)", config.DevLoginEmail())
	}

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
	if err := server.SetTrustedProxies(config.TrustedProxies); err != nil {
		log.Printf("SetTrustedProxies: %v — falling back to localhost only", err)
		_ = server.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	}
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 14,
		HttpOnly: true,
		SameSite: 2,
	})
	server.Use(sessions.Sessions("emailtracker_session", store))

	db.Prepare()
	log.Println("schema ready")
	model.MigrateLegacyStandardUsersToPro()
	model.MigrateAllUsersToPlanLimits()
	model.MigrateLegacyCampaignWorkflowStepTemplates()
	defer db.Close()

	engine := routes.InitWorkflowEngine()
	routes.StartCampaignScheduler()
	routes.StartWorkflowScheduler(engine)
	model.SyncAdminEmailsFromConfig()
	outbound.StartWorker()
	outbound.StartIMAPPoller()
	outbound.StartImportWorker()
	routes.RegisterRoutes(server)

	addr := ":" + config.Port
	log.Printf("listening on http://127.0.0.1%s (healthz ready)", addr)
	if err := server.Run(addr); err != nil {
		log.Fatalf("http server: %v", err)
	}
}
