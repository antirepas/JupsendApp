package routes

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

const defaultPilotDays = model.DefaultPilotDurationDays

// OpsCreatePilot grants Pro pilot access for an email (create or upgrade).
// Optional enter_now=1 starts impersonation immediately.
func OpsCreatePilot(c *gin.Context) {
	adminID := mustUserID(c)
	admin, _ := model.GetUserByID(adminID)
	email := strings.TrimSpace(strings.ToLower(c.PostForm("user_email")))
	enterNow := c.PostForm("enter_now") == "1" || c.PostForm("enter_now") == "on"

	userID, created, err := model.GrantPilotAccess(email, defaultPilotDays)
	if err != nil {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(err.Error()))
		return
	}

	target, _ := model.GetUserByID(userID)
	if created {
		log.Printf("ops pilot created admin=%d %s -> user=%d %s", adminID, admin.Email, userID, target.Email)
	} else {
		log.Printf("ops pilot upgraded admin=%d %s -> user=%d %s", adminID, admin.Email, userID, target.Email)
	}

	if enterNow {
		if err := startImpersonationAsAdmin(c, adminID, admin, userID, target); err != nil {
			c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(err.Error()))
			return
		}
		c.Redirect(http.StatusFound, "/")
		return
	}

	msg := "Pilot access granted for " + target.Email
	if created {
		msg = "Pilot account created for " + target.Email
	}
	c.Redirect(http.StatusFound, "/ops/provisioning?success="+url.QueryEscape(msg))
}

// OpsImpersonate enters a client's account (admin only).
func OpsImpersonate(c *gin.Context) {
	adminID := mustUserID(c)
	admin, _ := model.GetUserByID(adminID)
	email := strings.TrimSpace(strings.ToLower(c.PostForm("user_email")))
	if email == "" {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("email required"))
		return
	}
	target, err := model.GetUserByEmail(email)
	if err != nil {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("user not found"))
		return
	}
	if err := startImpersonationAsAdmin(c, adminID, admin, target.ID, target); err != nil {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func startImpersonationAsAdmin(c *gin.Context, adminID int64, admin model.User, targetID int64, target model.User) error {
	if auth.IsImpersonating(c) {
		return errAlreadyImpersonating
	}
	if targetID == adminID {
		return errCannotEnterSelf
	}
	if model.UserIsAdmin(target) {
		return errCannotEnterAdmin
	}
	auth.StartImpersonation(c, adminID, targetID)
	log.Printf("ops enter account admin=%d %s -> user=%d %s", adminID, admin.Email, targetID, target.Email)
	return nil
}

var (
	errAlreadyImpersonating = errors.New("already working as another account — exit first")
	errCannotEnterSelf      = errors.New("cannot enter your own account")
	errCannotEnterAdmin     = errors.New("cannot enter an admin account")
)

// ImpersonateExit restores the admin session.
func ImpersonateExit(c *gin.Context) {
	effectiveID, _ := auth.GetUserID(c)
	effective, _ := model.GetUserByID(effectiveID)
	adminID, ok := auth.StopImpersonation(c)
	if !ok {
		c.Redirect(http.StatusFound, "/")
		return
	}
	admin, _ := model.GetUserByID(adminID)
	log.Printf("ops exit account admin=%d %s <- user=%d %s", adminID, admin.Email, effectiveID, effective.Email)
	c.Redirect(http.StatusFound, "/ops/provisioning")
}

// AuthSessionAPI returns the effective session for the impersonation banner.
func AuthSessionAPI(c *gin.Context) {
	userID := mustUserID(c)
	user, err := model.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "authentication required"})
		return
	}
	impersonating := auth.IsImpersonating(c)
	c.JSON(http.StatusOK, gin.H{
		"impersonating": impersonating,
		"user_email":    user.Email,
		"user_id":       user.ID,
	})
}
