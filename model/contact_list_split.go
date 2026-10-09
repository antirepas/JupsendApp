package model

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
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

// ThinListResult is the outcome of randomly removing a percentage of list members.
type ThinListResult struct {
	RemovedCount  int
	KeptCount     int
	PoolCount     int
	MovedListID   int64
	MovedListName string
}

// ThinContactList randomly removes percent (1–99) of members from a list.
// When useFilter is true, only the current filter pool is considered; otherwise the whole list.
// If moveToName is set, removed contacts are copied into a new list before removal.
func ThinContactList(userID, listID int64, percent int, filter ListMembersFilter, useFilter bool, moveToName string) (ThinListResult, error) {
	var out ThinListResult
	if _, err := GetContactListForUser(listID, userID); err != nil {
		return out, err
	}
	if percent < 1 || percent > 99 {
		return out, fmt.Errorf("percent must be between 1 and 99")
	}

	var pool []int64
	var err error
	if useFilter {
		filter.Page = 1
		filter.PageSize = 10000
		pool, err = ListMemberIDsMatching(listID, userID, filter, 10000)
	} else {
		pool, err = ListMemberContactIDs(listID, userID)
	}
	if err != nil {
		return out, err
	}
	out.PoolCount = len(pool)
	if len(pool) == 0 {
		return out, fmt.Errorf("no contacts to thin")
	}

	nRemove := int(math.Round(float64(len(pool)) * float64(percent) / 100.0))
	if nRemove < 1 {
		nRemove = 1
	}
	if nRemove >= len(pool) {
		nRemove = len(pool) - 1
		if nRemove < 1 {
			return out, fmt.Errorf("list is too small to thin — need at least 2 contacts")
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	removeIDs := append([]int64(nil), pool[:nRemove]...)

	moveToName = strings.TrimSpace(moveToName)
	if moveToName != "" {
		movedID, err := CreateContactList(userID, moveToName)
		if err != nil {
			return out, err
		}
		if schema, _ := GetListVariableSchema(listID, userID); len(schema) > 0 {
			_ = SetListVariableSchema(movedID, userID, schema)
		}
		if err := AddContactsToList(movedID, userID, removeIDs); err != nil {
			return out, err
		}
		out.MovedListID = movedID
		out.MovedListName = moveToName
	}

	n, err := RemoveContactsFromList(listID, userID, removeIDs)
	if err != nil {
		return out, err
	}
	out.RemovedCount = n
	out.KeptCount = out.PoolCount - n
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
