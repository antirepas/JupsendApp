package routes

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emailtracker.com/config"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

type opsProvisionDomainRow struct {
	Domain        model.OutreachDomain
	UserEmail     string
	MailboxEmails []string
	Kind          string
}

type opsProvisionPurchaseRow struct {
	Purchase  model.MailboxPurchase
	UserEmail string
	Domain    string
	Summary   string
}

func OpsProvisioningPage(c *gin.Context) {
	userID := mustUserID(c)
	user, _ := model.GetUserByID(userID)
	domains, err := model.ListPendingManualDomains()
	if err != nil {
		log.Print(err)
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "error": "Failed to load queue"})
		return
	}
	purchases, err := model.ListPendingManualPurchases()
	if err != nil {
		log.Print(err)
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"title": "Error", "error": "Failed to load purchases"})
		return
	}

	var domainRows []opsProvisionDomainRow
	for _, d := range domains {
		row := opsProvisionDomainRow{Domain: d, Kind: "buy"}
		if strings.HasPrefix(d.InboxkitOrderID, "manual:connect:") {
			row.Kind = "connect"
		}
		if u, uErr := model.GetUserByID(d.UserID); uErr == nil {
			row.UserEmail = u.Email
		}
		if _, emails, sErr := model.SpecsFromDomainMailboxes(d.UserID, d.ID); sErr == nil {
			row.MailboxEmails = emails
		}
		domainRows = append(domainRows, row)
	}

	var purchaseRows []opsProvisionPurchaseRow
	for _, p := range purchases {
		row := opsProvisionPurchaseRow{Purchase: p}
		if u, uErr := model.GetUserByID(p.UserID); uErr == nil {
			row.UserEmail = u.Email
		}
		if p.DomainID > 0 {
			if d, dErr := model.GetOutreachDomain(p.DomainID, p.UserID); dErr == nil {
				row.Domain = d.Domain
			}
		}
		if payload, err := model.ParseDomainPurchasePayload(p.PayloadJSON); err == nil && payload.Domain != "" {
			row.Domain = payload.Domain
			row.Summary = "domain add-on"
		} else if specs, err := model.ParseMailboxSpecsJSON(p.PayloadJSON); err == nil {
			row.Summary = strconv.Itoa(len(specs)) + " mailbox seat(s)"
		}
		purchaseRows = append(purchaseRows, row)
	}

	c.HTML(http.StatusOK, "ops_provisioning.html", gin.H{
		"title":         "Manual provisioning",
		"active":        "settings",
		"user":          user,
		"isAdmin":       model.UserIsAdmin(user),
		"domains":       domainRows,
		"purchases":     purchaseRows,
		"manualEnabled": config.ManualInboxKitFulfillment,
		"includedSeats": config.InboxKitIncludedMailboxCount(),
		"mailboxSlots":  mailboxSlotNums(config.InboxKitIncludedMailboxCount()),
		"supportEmail":  config.SupportEmail,
		"success":       c.Query("success"),
		"error":         c.Query("error"),
	})
}

func OpsAdminConnectDomain(c *gin.Context) {
	userEmail := strings.TrimSpace(c.PostForm("user_email"))
	domain := strings.TrimSpace(c.PostForm("domain"))
	if userEmail == "" || domain == "" {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("User email and domain are required"))
		return
	}
	u, err := model.GetUserByEmail(userEmail)
	if err != nil {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("User not found: "+userEmail))
		return
	}
	n := config.InboxKitIncludedMailboxCount()
	var specs []model.StarterMailboxSpec
	for i := 1; i <= n; i++ {
		fn := strings.TrimSpace(c.PostForm(fmt.Sprintf("first_name_%d", i)))
		ln := strings.TrimSpace(c.PostForm(fmt.Sprintf("last_name_%d", i)))
		local := strings.TrimSpace(c.PostForm(fmt.Sprintf("local_%d", i)))
		if fn == "" && ln == "" && local == "" {
			continue
		}
		if fn == "" || ln == "" || local == "" {
			c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(fmt.Sprintf("Mailbox %d needs first name, last name, and local part", i)))
			return
		}
		specs = append(specs, model.StarterMailboxSpec{FirstName: fn, LastName: ln, LocalPart: local})
	}
	if len(specs) == 0 {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("Add at least one mailbox"))
		return
	}
	_, detail, err := model.AdminConnectDomainWithMailboxes(u.ID, domain, specs)
	if err != nil {
		log.Printf("ops admin connect %s for %s: %v", domain, userEmail, err)
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/ops/provisioning?success="+url.QueryEscape(detail))
}

func OpsFulfillDomain(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("Invalid domain id"))
		return
	}
	if err := model.FulfillQueuedDomainOrder(id); err != nil {
		log.Printf("ops fulfill domain %d: %v", id, err)
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/ops/provisioning?success="+url.QueryEscape("Domain sent to InboxKit — wait for ready, customer gets an email when mailboxes are live"))
}

func OpsFulfillPurchase(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("Invalid purchase id"))
		return
	}
	p, err := model.GetMailboxPurchase(id)
	if err != nil {
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape("Purchase not found"))
		return
	}
	var fulfillErr error
	if payload, pErr := model.ParseDomainPurchasePayload(p.PayloadJSON); pErr == nil && payload.Domain != "" {
		fulfillErr = FulfillDomainPurchase(id)
	} else {
		fulfillErr = FulfillMailboxPurchase(id)
	}
	if fulfillErr != nil {
		log.Printf("ops fulfill purchase %d: %v", id, fulfillErr)
		c.Redirect(http.StatusFound, "/ops/provisioning?error="+url.QueryEscape(fulfillErr.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/ops/provisioning?success="+url.QueryEscape("Purchase fulfilled"))
}
