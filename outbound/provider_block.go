package outbound

import (
	"sync"
	"time"
)

// providerBlockUntil tracks mailboxes that hit provider daily/rate limits
// (e.g. Gmail 5.4.5) so routing can failover to another seat until reset.
var (
	providerBlockMu sync.Mutex
	providerBlocked = map[int64]time.Time{}
)

// MarkAccountProviderBlocked skips this mailbox for routing until untilTime.
func MarkAccountProviderBlocked(accountID int64, until time.Time) {
	if accountID <= 0 || until.Before(time.Now()) {
		return
	}
	providerBlockMu.Lock()
	defer providerBlockMu.Unlock()
	if prev, ok := providerBlocked[accountID]; ok && prev.After(until) {
		return
	}
	providerBlocked[accountID] = until
}

func IsAccountProviderBlocked(accountID int64) bool {
	if accountID <= 0 {
		return false
	}
	providerBlockMu.Lock()
	defer providerBlockMu.Unlock()
	until, ok := providerBlocked[accountID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(providerBlocked, accountID)
		return false
	}
	return true
}

func clearExpiredProviderBlocks() {
	providerBlockMu.Lock()
	defer providerBlockMu.Unlock()
	now := time.Now()
	for id, until := range providerBlocked {
		if now.After(until) {
			delete(providerBlocked, id)
		}
	}
}
