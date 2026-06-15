package outbound

import (
	"os"
	"strconv"
	"time"
)

var (
	WorkerInterval  = 8 * time.Second
	IMAPPollInterval = 3 * time.Minute
	BatchSize       = 10
	LockDuration    = 2 * time.Minute
)

func LoadConfig() {
	if v := os.Getenv("OUTBOUND_WORKER_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			WorkerInterval = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("IMAP_POLL_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			IMAPPollInterval = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("OUTBOUND_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			BatchSize = n
		}
	}
}
