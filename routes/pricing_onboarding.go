package routes

import (
	"net/http"
	"strings"

	"emailtracker.com/auth"
	"emailtracker.com/config"
	"emailtracker.com/googleoauth"
	"emailtracker.com/model"
	"emailtracker.com/whop"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const appGooglePlanSessionKey = "app_google_plan"
const appGoogleStateSessionKey = "app_google_state"

func SignupPlanPage(c *gin.Context) {
	plan := c.Param("plan")
	var tier model.PlanTier
	switch strings.ToLower(plan) {
	case string(model.PlanTierFree):
		tier = model.PlanTierFree
	case string(model.PlanTierStandard):
		tier = model.PlanTierStandard
	case string(model.PlanTierPro):
		tier = model.PlanTierPro
	default:
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}

	spec, err := model.PlanSpecForTier(tier)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}

	planLower := strings.ToLower(plan)
	planDisplay := planLower
	if len(planLower) > 0 {
		planDisplay = strings.ToUpper(planLower[:1]) + planLower[1:]
	}

	c.HTML(http.StatusOK, "pricing_plan.html", gin.H{
		"title":             "Pricing: " + planDisplay,
		"active":           "pricing",
		"plan":             plan,
		"dailyEmails":      spec.DailyEmailCap,
		"aiCreditsPerDay":  spec.AICreditsPerDay,
		"warmup":           spec.WarmupEnabled,
		"stripeLikeNote":   "Whop checkout is used for Standard/Pro.",
	})
}

func AppGoogleStart(c *gin.Context) {
	plan := strings.TrimSpace(c.Query("plan"))
	tier := model.PlanTier(strings.ToLower(plan))
	if tier != model.PlanTierFree && tier != model.PlanTierStandard && tier != model.PlanTierPro {
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}
	if !googleoauth.IsConfigured() {
		c.Redirect(http.StatusFound, "/settings?error=Google+OAuth+not+configured")
		return
	}

	state := randomNonce()
	s := sessions.Default(c)
	s.Set(appGoogleStateSessionKey, state)
	s.Set(appGooglePlanSessionKey, string(tier))
	_ = s.Save()

	c.Redirect(http.StatusFound, googleoauth.AppAuthURL(state))
}

func AppGoogleCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")
	if errMsg := c.Query("error"); errMsg != "" {
		c.Redirect(http.StatusFound, "/signup/free?error=Google+OAuth+cancelled")
		return
	}
	if code == "" || state == "" {
		c.Redirect(http.StatusFound, "/signup/free?error=Invalid+OAuth+response")
		return
	}

	s := sessions.Default(c)
	expected, _ := s.Get(appGoogleStateSessionKey).(string)
	planStr, _ := s.Get(appGooglePlanSessionKey).(string)
	s.Delete(appGoogleStateSessionKey)
	s.Delete(appGooglePlanSessionKey)
	_ = s.Save()

	if expected == "" || expected != state {
		c.Redirect(http.StatusFound, "/signup/free?error=OAuth+state+mismatch")
		return
	}

	tier := model.PlanTier(strings.ToLower(planStr))
	if tier != model.PlanTierFree && tier != model.PlanTierStandard && tier != model.PlanTierPro {
		c.Redirect(http.StatusFound, "/signup/free?error=Unknown+plan")
		return
	}

	tok, profile, err := googleoauth.AppExchangeCode(c.Request.Context(), code)
	if err != nil || profile.Email == "" {
		c.Redirect(http.StatusFound, "/signup/free?error=Could+not+complete+Google+sign-in")
		return
	}
	if tok.RefreshToken == "" {
		// Without refresh token we can't store Gmail credentials for later sending.
		c.Redirect(http.StatusFound, "/signup/free?error=Google+did+not+return+refresh+token")
		return
	}

	normalizedEmail := strings.TrimSpace(strings.ToLower(profile.Email))
	userID, err := ensureUserForGoogle(normalizedEmail)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup/free?error=Could+not+create+account")
		return
	}

	auth.SetUserSession(c, userID)
	_ = model.ApplyPlanLimitsToUser(userID, tier)

	fromName := profile.Name
	if fromName == "" {
		fromName = normalizedEmail
	}

	encRefresh, err := googleoauth.Encrypt(tok.RefreshToken)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup/free?error=Could+not+store+Google+token")
		return
	}
	encAccess, _ := googleoauth.Encrypt(tok.AccessToken)

	// Store Gmail OAuth tokens (sending + bounce tracking later).
	if err := model.SaveGoogleOAuthAccount(userID, normalizedEmail, fromName, encRefresh, encAccess, tok.Expiry); err != nil {
		c.Redirect(http.StatusFound, "/signup/free?error=Could+not+save+Gmail+account")
		return
	}
	_ = model.ApplyPlanLimitsToUser(userID, tier)

	if tier == model.PlanTierFree {
		_ = model.UpdateUserSubscription(userID, model.SubStatusActive, "", "", nil)
		c.Redirect(http.StatusFound, "/")
		return
	}

	_ = model.UpdateUserSubscription(userID, model.SubStatusPendingPayment, "", "", nil)

	redirectURL := strings.TrimRight(model.UserBaseURL(userID), "/") + "/onboarding/activate"
	purchaseURL, err := whop.CreateCheckout(userID, tier, redirectURL)
	if err != nil {
		c.Redirect(http.StatusFound, "/settings/billing?error="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, purchaseURL)
}

func OnboardingActivatePage(c *gin.Context) {
	userID := mustUserID(c)
	if userID == 0 {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	u, err := model.GetUserByID(userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	if model.UserHasAppAccess(u) {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.HTML(http.StatusOK, "onboarding_activating.html", gin.H{
		"title":   "Activating",
		"user":    u,
		"plan":    u.PlanTier,
		"status":  u.SubscriptionStatus,
	})
}

func ensureUserForGoogle(email string) (int64, error) {
	if user, err := model.GetUserByEmail(email); err == nil {
		return user.ID, nil
	}
	randomPassword := randomNonce() // any sufficiently long value is fine
	passHash, err := auth.HashPassword(randomPassword)
	if err != nil {
		return 0, err
	}
	return model.CreateUser(email, passHash, config.BaseURL)
}

func urlQueryEscape(s string) string {
	// Minimal escape for query strings without importing net/url in every file.
	s = strings.ReplaceAll(s, " ", "+")
	return s
}

