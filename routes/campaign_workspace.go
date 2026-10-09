package routes

import (
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
)

// workspaceNavVars builds template vars for the campaign folder carousel.
// currentKind: manage | template | contacts | workflow
func workspaceNavVars(userID, folderID, campaignID int64, currentKind string, currentID int64) gin.H {
	out := gin.H{}
	if folderID <= 0 || campaignID <= 0 {
		return out
	}
	items, err := model.BuildCampaignWorkspaceNav(userID, folderID, campaignID)
	if err != nil || len(items) == 0 {
		return out
	}
	idx := -1
	for i, it := range items {
		if it.Kind == currentKind && it.ID == currentID {
			idx = i
			break
		}
	}
	if idx < 0 && currentKind == "manage" {
		idx = 0
	}
	if idx < 0 {
		return out
	}
	out["workspaceNav"] = true
	out["workspaceNavItems"] = items
	out["workspaceIndex"] = idx + 1
	out["workspaceTotal"] = len(items)
	out["workspaceCurrentLabel"] = items[idx].Label
	out["workspaceFolderID"] = folderID
	out["workspaceCampaignID"] = campaignID
	if idx > 0 {
		out["workspacePrevURL"] = items[idx-1].URL
	}
	if idx+1 < len(items) {
		out["workspaceNextURL"] = items[idx+1].URL
	}
	return out
}

func mergeWorkspaceNav(dst gin.H, nav gin.H) {
	for k, v := range nav {
		dst[k] = v
	}
}

// resolveFolderCampaignNav finds campaign+folder for a library asset and returns nav vars.
func resolveFolderCampaignNav(userID, folderID int64, kind string, assetID int64) gin.H {
	if folderID <= 0 {
		return gin.H{}
	}
	c, err := model.GetCampaignByLibraryFolder(folderID, userID)
	if err != nil || c.ID <= 0 {
		id, err := model.EnsureCampaignForLibraryFolder(userID, folderID, "")
		if err != nil || id <= 0 {
			return gin.H{}
		}
		c.ID = id
		c.LibraryFolderID = folderID
	}
	return workspaceNavVars(userID, folderID, c.ID, kind, assetID)
}
