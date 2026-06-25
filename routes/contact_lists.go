package routes

import (
	"net/http"
	"strconv"

	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

func CreateContactList(c *gin.Context) {
	userID := mustUserID(c)
	name := c.PostForm("name")
	if name == "" {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=List+name+required")
		return
	}
	if _, err := model.CreateContactList(userID, name); err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Could+not+create+list")
		return
	}
	c.Redirect(http.StatusFound, "/contacts?tab=lists&success=List+created")
}

func RenameContactList(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	name := c.PostForm("name")
	if err := model.RenameContactList(listID, userID, name); err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Could+not+rename+list")
		return
	}
	c.Redirect(http.StatusFound, "/contacts?tab=lists&success=List+renamed")
}

func DeleteContactList(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	if err := model.DeleteContactList(listID, userID); err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Could+not+delete+list")
		return
	}
	c.Redirect(http.StatusFound, "/contacts?tab=lists&success=List+deleted")
}

func AddListMembers(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?error=Invalid+list")
		return
	}
	ids := parseContactIDs(c)
	if err := model.AddContactsToList(listID, userID, ids); err != nil {
		c.Redirect(http.StatusFound, "/contacts?error=Could+not+add+to+list")
		return
	}
	c.Redirect(http.StatusFound, "/contacts?list="+strconv.FormatInt(listID, 10)+"&success=Contacts+added+to+list")
}

func RemoveListMember(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?error=Invalid+list")
		return
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?error=Invalid+contact")
		return
	}
	_ = model.RemoveContactFromList(listID, userID, contactID)
	c.Redirect(http.StatusFound, "/contacts/"+strconv.FormatInt(contactID, 10)+"?success=Removed+from+list")
}

func AddCampaignList(c *gin.Context) {
	userID := mustUserID(c)
	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	listID, err := strconv.ParseInt(c.PostForm("list_id"), 10, 64)
	if err != nil || listID == 0 {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Select+a+list")
		return
	}
	n, err := model.SnapshotListToCampaign(listID, campaignID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Could+not+add+list")
		return
	}
	msg := "Added " + strconv.Itoa(n) + " contacts from list"
	c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+msg)
}

func RefreshCampaignList(c *gin.Context) {
	userID := mustUserID(c)
	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns?error=Invalid+campaign")
		return
	}
	campaign, err := model.GetCampaignForUser(campaignID, userID)
	if err != nil || campaign.ContactListID == 0 {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=No+list+linked")
		return
	}
	n, err := model.SnapshotListToCampaign(campaign.ContactListID, campaignID, userID)
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error=Could+not+refresh+list")
		return
	}
	msg := "Refreshed " + strconv.Itoa(n) + " contacts from list"
	c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+msg)
}
