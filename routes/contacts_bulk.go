package routes

import (
	"net/http"
	"net/url"
	"strconv"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func InterestedBulkAddList(c *gin.Context) {
	userID := mustUserID(c)
	listID, _ := strconv.ParseInt(c.PostForm("list_id"), 10, 64)
	ids := parseContactIDs(c)
	campaign := c.Query("campaign")
	if campaign == "" {
		campaign = c.PostForm("campaign")
	}
	base := "/inbox?folder=interested"
	if campaign != "" {
		base += "&campaign=" + url.QueryEscape(campaign)
	}
	if listID <= 0 || len(ids) == 0 {
		c.Redirect(http.StatusFound, base+"&error="+url.QueryEscape("Select contacts and a list"))
		return
	}
	if err := model.AddContactsToList(listID, userID, ids); err != nil {
		c.Redirect(http.StatusFound, base+"&error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, base+"&success="+url.QueryEscape("Added "+strconv.Itoa(len(ids))+" contacts to list"))
}

func InterestedBulkSuppress(c *gin.Context) {
	userID := mustUserID(c)
	ids := parseContactIDs(c)
	if len(ids) == 0 {
		c.Redirect(http.StatusFound, "/inbox?folder=interested&error="+url.QueryEscape("Select contacts to suppress"))
		return
	}
	n, _ := model.BulkSuppressContacts(userID, ids, "manual")
	c.Redirect(http.StatusFound, "/inbox?folder=interested&success="+url.QueryEscape("Suppressed "+strconv.Itoa(n)+" contacts"))
}

func InterestedBulkDismiss(c *gin.Context) {
	userID := mustUserID(c)
	ids := parseContactIDs(c)
	if len(ids) == 0 {
		c.Redirect(http.StatusFound, "/inbox?folder=interested&error="+url.QueryEscape("Select contacts to dismiss"))
		return
	}
	n, _ := model.DismissInterestedContacts(userID, ids)
	c.Redirect(http.StatusFound, "/inbox?folder=interested&success="+url.QueryEscape("Dismissed "+strconv.Itoa(n)+" from queue"))
}
