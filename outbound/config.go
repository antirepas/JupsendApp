package outbound

import (
	"os"
	"strconv"
	"time"

	"emailtracker.com/util"
)

const (
	PriorityManual   = 10
	PriorityCampaign = 0
)

var (
	WorkerInterval   = 3 * time.Second
	IMAPPollInterval = 3 * time.Minute
	BatchSize        = 10
	LockDuration     = 45 * time.Second
	SMTPSendTimeout  = 30 * time.Second
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
	if v := os.Getenv("OUTBOUND_LOCK_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			LockDuration = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("OUTBOUND_SMTP_TIMEOUT_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			SMTPSendTimeout = time.Duration(secs) * time.Second
		}
	}
	util.DefaultSMTPSendTimeout = SMTPSendTimeout
}
