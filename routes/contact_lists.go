package routes

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emailtracker.com/model"
	"emailtracker.com/outbound"
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
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success=Contacts+added+to+list")
}

func ContactListDetailPage(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	pageNum, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	filter := model.ListMembersFilter{
		Query:      c.Query("q"),
		Engagement: c.Query("engagement"),
		Sort:       c.DefaultQuery("sort", "email"),
		Page:       pageNum,
		PageSize:   50,
	}
	memberPage, err := model.ListContactsInListPage(listID, userID, filter)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=List+not+found")
		return
	}
	columns, _ := model.GetListVariableSchema(listID, userID)
	allContacts, _ := model.ListContactPickerItems(userID, 2000)
	memberIDs, _ := model.ListMemberContactIDs(listID, userID)
	memberSet := map[int64]bool{}
	for _, id := range memberIDs {
		memberSet[id] = true
	}
	var addable []model.ContactListItem
	for _, ct := range allContacts {
		if !memberSet[ct.ID] {
			addable = append(addable, ct)
		}
	}
	hasPrev := memberPage.Page > 1
	hasNext := memberPage.TotalPages > 0 && memberPage.Page < memberPage.TotalPages
	c.HTML(http.StatusOK, "contacts_list_detail.html", gin.H{
		"title":            memberPage.List.Name,
		"active":           "contacts",
		"list":             memberPage.List,
		"rows":             memberPage.Items,
		"columns":          columns,
		"memberPage":       memberPage,
		"filterQ":          filter.Query,
		"filterEngagement": filter.Engagement,
		"filterSort":       filter.Sort,
		"hasPrev":          hasPrev,
		"hasNext":          hasNext,
		"prevPage":         memberPage.Page - 1,
		"nextPage":         memberPage.Page + 1,
		"addContacts":      addable,
		"success":          c.Query("success"),
		"error":            c.Query("error"),
	})
}

func SetContactListSchema(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	raw := c.PostForm("schema")
	keys := model.ParseSchemaKeysInput(raw)
	if err := model.SetListVariableSchema(listID, userID, keys); err != nil {
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success=Schema+updated")
}

func ListVariablesJSON(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid list"})
		return
	}
	keys, sample, err := model.ListVariableSample(listID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "list not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"variable_keys": keys,
		"sample":        sample,
	})
}

func ContactVariablesJSON(c *gin.Context) {
	userID := mustUserID(c)
	contactID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid contact"})
		return
	}
	sample, err := model.ContactVariableSample(userID, contactID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contact not found"})
		return
	}
	keys := make([]string, 0, len(sample))
	for k := range sample {
		keys = append(keys, k)
	}
	c.JSON(http.StatusOK, gin.H{
		"variable_keys": keys,
		"sample":        sample,
	})
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
	c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success=Removed+from+list")
}

func BulkListMembers(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	ids := parseContactIDs(c)
	action := strings.TrimSpace(c.PostForm("action"))
	switch action {
	case "delete":
		n, _ := model.BulkDeleteContacts(userID, ids)
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success="+url.QueryEscape("Deleted "+strconv.Itoa(n)+" leads"))
	default:
		n, _ := model.RemoveContactsFromList(listID, userID, ids)
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success="+url.QueryEscape("Removed "+strconv.Itoa(n)+" from list"))
	}
}

func DeleteListMemberContact(c *gin.Context) {
	userID := mustUserID(c)
	listID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts?tab=lists&error=Invalid+list")
		return
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?error=Invalid+contact")
		return
	}
	if err := model.DeleteContact(contactID, userID); err != nil {
		c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?error="+url.QueryEscape("Could not delete lead"))
		return
	}
	c.Redirect(http.StatusFound, "/contacts/lists/"+strconv.FormatInt(listID, 10)+"?success=Lead+deleted")
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
	if _, err := model.GetCampaignForUser(campaignID, userID); err != nil {
		c.Redirect(http.StatusFound, "/campaigns?error=Campaign+not+found")
		return
	}
	job, err := model.EnqueueImportJob(userID, model.ImportKindCampaignListSnapshot, model.ImportJobPayload{
		CampaignID:     campaignID,
		SnapshotListID: listID,
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	outbound.NotifyImportWorker()
	msg := "Adding list in the background (" + strconv.Itoa(job.TotalRows) + " contacts). Progress shows in the top banner."
	c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
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
	job, err := model.EnqueueImportJob(userID, model.ImportKindCampaignListSnapshot, model.ImportJobPayload{
		CampaignID:     campaignID,
		SnapshotListID: campaign.ContactListID,
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?error="+url.QueryEscape(err.Error()))
		return
	}
	outbound.NotifyImportWorker()
	msg := "Refreshing list in the background (" + strconv.Itoa(job.TotalRows) + " contacts)."
	c.Redirect(http.StatusFound, "/campaigns/"+strconv.FormatInt(campaignID, 10)+"?success="+url.QueryEscape(msg))
}
