package routes

import (
	"net/http"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func DevLogin(c *gin.Context) {
	if !config.IsLocalDev() {
		c.Redirect(http.StatusFound, "/login?error=Not+available")
		return
	}

	email := strings.TrimSpace(strings.ToLower(c.PostForm("email")))
	if email == "" {
		email = strings.TrimSpace(strings.ToLower(c.Query("email")))
	}
	if email == "" {
		email = config.DevLoginEmail()
	}

	userID, err := ensureDevUser(email)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=Dev+login+failed")
		return
	}

	auth.SetUserSession(c, userID)
	_ = model.SetUserAdmin(userID, config.IsAdminEmail(email))
	if config.IsAdminEmail(email) {
		_ = model.EnsureAdminProAccess(userID)
	}
	c.Redirect(http.StatusFound, safeNext(c.Query("next")))
}

func ensureDevUser(email string) (int64, error) {
	if user, err := model.GetUserByEmail(email); err == nil {
		if config.IsAdminEmail(email) {
			_ = model.EnsureAdminProAccess(user.ID)
		} else {
			_ = model.ApplyPlanLimitsToUser(user.ID, model.PlanTierFree)
		}
		_ = model.UpdateUserSubscription(user.ID, model.SubStatusActive, "", "", nil)
		return user.ID, nil
	}
	userID, err := ensureUserForGoogle(email)
	if err != nil {
		return 0, err
	}
	_ = model.ApplyPlanLimitsToUser(userID, model.PlanTierFree)
	_ = model.UpdateUserSubscription(userID, model.SubStatusActive, "", "", nil)
	return userID, nil
}
