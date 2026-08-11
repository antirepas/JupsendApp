package model

import (
	"log"
	"sync"
	"time"
)

var (
	adminOutreachEnsureMu   sync.Mutex
	adminOutreachEnsureAt   = map[int64]time.Time{}
	pendingOutreachSyncMu   sync.Mutex
	pendingOutreachSyncAt   = map[int64]time.Time{}
	adminOutreachEnsureMin  = 2 * time.Minute
	pendingOutreachSyncMin  = 30 * time.Second
)

// ScheduleAdminOutreachEnsure runs EnsureAdminOutreachDomain in the background at most
// once per adminOutreachEnsureMin so /mailboxes stays fast.
func ScheduleAdminOutreachEnsure(userID int64) {
	if userID <= 0 {
		return
	}
	adminOutreachEnsureMu.Lock()
	last := adminOutreachEnsureAt[userID]
	if time.Since(last) < adminOutreachEnsureMin {
		adminOutreachEnsureMu.Unlock()
		return
	}
	adminOutreachEnsureAt[userID] = time.Now()
	adminOutreachEnsureMu.Unlock()

	go func(uid int64) {
		if err := EnsureAdminOutreachDomain(uid); err != nil {
			log.Printf("admin outreach domain (bg): %v", err)
		}
	}(userID)
}

// SchedulePendingOutreachSync runs SyncPendingOutreachDomains in the background,
// debounced so ready domains don't block page renders.
func SchedulePendingOutreachSync(userID int64) {
	if userID <= 0 {
		return
	}
	pendingOutreachSyncMu.Lock()
	last := pendingOutreachSyncAt[userID]
	if time.Since(last) < pendingOutreachSyncMin {
		pendingOutreachSyncMu.Unlock()
		return
	}
	pendingOutreachSyncAt[userID] = time.Now()
	pendingOutreachSyncMu.Unlock()

	go func(uid int64) {
		SyncPendingOutreachDomains(uid)
	}(userID)
}
