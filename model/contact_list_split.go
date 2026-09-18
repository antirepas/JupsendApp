package model

import (
	"fmt"
	"strings"
)

// SplitListResult is the outcome of splitting a list into match / rest segments.
type SplitListResult struct {
	MatchListID   int64
	RestListID    int64
	MatchListName string
	RestListName  string
	MatchCount    int
	RestCount     int
}

// SplitContactList copies members matching filter into matchName and everyone else into restName.
// Contacts remain on the source list unless removeFromSource is true.
func SplitContactList(userID, listID int64, matchName, restName string, filter ListMembersFilter, removeFromSource bool) (SplitListResult, error) {
	var out SplitListResult
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return out, err
	}
	matchName = strings.TrimSpace(matchName)
	restName = strings.TrimSpace(restName)
	if matchName == "" || restName == "" {
		return out, fmt.Errorf("both segment names are required")
	}
	if strings.EqualFold(matchName, restName) {
		return out, fmt.Errorf("segment names must be different")
	}
	// Clear pagination — we need all matching IDs.
	filter.Page = 1
	filter.PageSize = 5000

	matchIDs, err := ListMemberIDsMatching(listID, userID, filter, 5000)
	if err != nil {
		return out, err
	}
	allIDs, err := ListMemberContactIDs(listID, userID)
	if err != nil {
		return out, err
	}
	matchSet := map[int64]bool{}
	for _, id := range matchIDs {
		matchSet[id] = true
	}
	var restIDs []int64
	for _, id := range allIDs {
		if !matchSet[id] {
			restIDs = append(restIDs, id)
		}
	}

	matchListID, err := CreateContactList(userID, matchName)
	if err != nil {
		return out, err
	}
	restListID, err := CreateContactList(userID, restName)
	if err != nil {
		return out, err
	}

	schema, _ := GetListVariableSchema(listID, userID)
	if len(schema) > 0 {
		_ = SetListVariableSchema(matchListID, userID, schema)
		_ = SetListVariableSchema(restListID, userID, schema)
	}

	if err := AddContactsToList(matchListID, userID, matchIDs); err != nil {
		return out, err
	}
	if err := AddContactsToList(restListID, userID, restIDs); err != nil {
		return out, err
	}

	if removeFromSource {
		_, _ = RemoveContactsFromList(listID, userID, allIDs)
	}

	out = SplitListResult{
		MatchListID:   matchListID,
		RestListID:    restListID,
		MatchListName: matchName,
		RestListName:  restName,
		MatchCount:    len(matchIDs),
		RestCount:     len(restIDs),
	}
	return out, nil
}

// SaveMatchingAsList copies filter matches from a source list into a new list.
func SaveMatchingAsList(userID, listID int64, name string, filter ListMembersFilter) (newListID int64, count int, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, 0, fmt.Errorf("list name required")
	}
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return 0, 0, err
	}
	ids, err := ListMemberIDsMatching(listID, userID, filter, 5000)
	if err != nil {
		return 0, 0, err
	}
	newListID, err = CreateContactList(userID, name)
	if err != nil {
		return 0, 0, err
	}
	if schema, _ := GetListVariableSchema(listID, userID); len(schema) > 0 {
		_ = SetListVariableSchema(newListID, userID, schema)
	}
	if err := AddContactsToList(newListID, userID, ids); err != nil {
		return newListID, 0, err
	}
	return newListID, len(ids), nil
}
