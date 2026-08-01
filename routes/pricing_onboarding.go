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
const appGoogleModeSessionKey = "app_google_mode"
const appGoogleNextSessionKey = "app_google_next"
const appGoogleModeLogin = "login"
const appGoogleModeSignup = "signup"

func SignupPlanPage(c *gin.Context) {
	plan := strings.ToLower(c.Param("plan"))
	if plan == "standard" {
		c.Redirect(http.StatusFound, "/signup/pro")
		return
	}
	tier := model.NormalizePlanTier(plan)
	if plan != string(model.PlanTierFree) && plan != string(model.PlanTierPro) {
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}

	spec, err := model.PlanSpecForTier(tier)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}

	planDisplay := "Free"
	if tier == model.PlanTierPro {
		planDisplay = "Pro"
	}

	c.HTML(http.StatusOK, "pricing_plan.html", gin.H{
		"title":            "Pricing: " + planDisplay,
		"active":           "pricing",
		"plan":             string(tier),
		"dailyEmails":      spec.DailyEmailCap,
		"aiCreditsPerDay":  spec.AICreditsPerDay,
		"warmup":           spec.WarmupEnabled,
		"includedDomains":  spec.IncludedDomains,
		"includedMailboxes": spec.IncludedMailboxes,
		"stripeLikeNote":   "Whop checkout is used for Pro.",
	})
}

func AppGoogleLoginStart(c *gin.Context) {
	if !googleoauth.IsConfigured() {
		c.Redirect(http.StatusFound, "/login?error=Google+sign-in+not+configured")
		return
	}
	state := randomNonce()
	s := sessions.Default(c)
	s.Set(appGoogleStateSessionKey, state)
	s.Set(appGoogleModeSessionKey, appGoogleModeLogin)
	s.Set(appGoogleNextSessionKey, c.Query("next"))
	_ = s.Save()
	c.Redirect(http.StatusFound, googleoauth.AppAuthURL(state))
}

func AppGoogleStart(c *gin.Context) {
	plan := strings.TrimSpace(c.Query("plan"))
	if strings.EqualFold(plan, "standard") {
		plan = string(model.PlanTierPro)
	}
	tier := model.NormalizePlanTier(plan)
	if plan != string(model.PlanTierFree) && plan != string(model.PlanTierPro) {
		c.Redirect(http.StatusFound, "/signup/free")
		return
	}
	if !googleoauth.IsConfigured() {
		c.Redirect(http.StatusFound, "/signup/free?error=Google+sign-in+not+configured")
		return
	}

	state := randomNonce()
	s := sessions.Default(c)
	s.Set(appGoogleStateSessionKey, state)
	s.Set(appGoogleModeSessionKey, appGoogleModeSignup)
	s.Set(appGooglePlanSessionKey, string(tier))
	_ = s.Save()

	c.Redirect(http.StatusFound, googleoauth.AppAuthURL(state))
}

func AppGoogleCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")

	s := sessions.Default(c)
	expected, _ := s.Get(appGoogleStateSessionKey).(string)
	mode, _ := s.Get(appGoogleModeSessionKey).(string)
	planStr, _ := s.Get(appGooglePlanSessionKey).(string)
	next, _ := s.Get(appGoogleNextSessionKey).(string)
	s.Delete(appGoogleStateSessionKey)
	s.Delete(appGoogleModeSessionKey)
	s.Delete(appGooglePlanSessionKey)
	s.Delete(appGoogleNextSessionKey)
	_ = s.Save()

	if mode == "" {
		mode = appGoogleModeSignup
	}
	oauthErrRedirect := func(msg string) {
		if mode == appGoogleModeLogin {
			c.Redirect(http.StatusFound, "/login?error="+urlQueryEscape(msg))
			return
		}
		c.Redirect(http.StatusFound, "/signup/free?error="+urlQueryEscape(msg))
	}

	if errMsg := c.Query("error"); errMsg != "" {
		oauthErrRedirect("Google sign-in cancelled")
		return
	}
	if code == "" || state == "" {
		oauthErrRedirect("Invalid OAuth response")
		return
	}
	if expected == "" || expected != state {
		oauthErrRedirect("OAuth state mismatch")
		return
	}

	_, profile, err := googleoauth.AppExchangeCode(c.Request.Context(), code)
	if err != nil || profile.Email == "" {
		oauthErrRedirect("Could not complete Google sign-in")
		return
	}

	normalizedEmail := strings.TrimSpace(strings.ToLower(profile.Email))
	if mode == appGoogleModeLogin {
		appGoogleLoginComplete(c, normalizedEmail, next)
		return
	}

	tier := model.NormalizePlanTier(planStr)
	if planStr != "" && planStr != string(model.PlanTierFree) && planStr != string(model.PlanTierPro) && !strings.EqualFold(planStr, "standard") {
		oauthErrRedirect("Unknown plan")
		return
	}

	userID, err := ensureUserForGoogle(normalizedEmail)
	if err != nil {
		oauthErrRedirect("Could not create account")
		return
	}

	auth.SetUserSession(c, userID)
	if err := model.ApplyPlanLimitsToUser(userID, tier); err != nil && tier == model.PlanTierFree {
		oauthErrRedirect("Free plan sending is not configured yet: " + err.Error())
		return
	}

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

func appGoogleLoginComplete(c *gin.Context, email, next string) {
	user, err := model.GetUserByEmail(email)
	if err != nil {
		c.Redirect(http.StatusFound, "/signup/free?error=No+account+found.+Choose+a+plan+to+sign+up.")
		return
	}

	auth.SetUserSession(c, user.ID)
	_ = model.SetUserAdmin(user.ID, config.IsAdminEmail(user.Email))
	// Re-apply Free shared SMTP if needed (no Gmail send account — Google is identity only).
	if model.NormalizePlanTier(user.PlanTier) == model.PlanTierFree {
		_ = model.ApplyPlanLimitsToUser(user.ID, model.PlanTierFree)
	}

	c.Redirect(http.StatusFound, safeNext(next))
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

