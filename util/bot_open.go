package util

import (
	"net"
	"strings"
	"time"
)

// MinHumanOpenDelay flags pixel hits that arrive sooner than this after delivery as scanners.
const MinHumanOpenDelay = 5 * time.Second

// OpenClassification is the result of bot/scanner heuristics for an open pixel hit.
type OpenClassification struct {
	IsBot  bool
	Reason string // user_agent | too_fast | datacenter_ip | ""
}

var botUserAgentNeedles = []string{
	// NOTE: Do NOT list Gmail/Yahoo image proxies (GoogleImageProxy, ggpht.com, YahooMailProxy).
	// Those UAs are how real humans load pixels in webmail — treating them as bots zeros open rates.
	"appleprivacy",
	"privacyprotection",
	"barracuda",
	"mimecast",
	"proofpoint",
	"urldefense",
	"messagelabs",
	"symantec",
	"trend micro",
	"fireeye",
	"mandiant",
	"cisco ironport",
	"ironport",
	"forcepoint",
	"spamassassin",
	"mailscanner",
	"postfix",
	"mime-filter",
	"safelinks",
	"protection.outlook",
	"microsoft office excel", // link preview bots often use Office UAs oddly; keep narrow
	"facebookexternalhit",
	"twitterbot",
	"linkedinbot",
	"slackbot",
	"discordbot",
	"curl/",
	"wget/",
	"python-requests",
	"go-http-client",
	"java/",
	"libwww-perl",
	"scrapy",
	"headlesschrome",
	"phantomjs",
	"spider",
	"crawler",
	"bot/",
	"bot;",
}

// Known scanner / security gateway prefixes (not exhaustive — UA + too_fast catch most).
var scannerCIDRs = mustParseCIDRs(
	// Barracuda
	"64.235.144.0/20",
	"205.220.160.0/19",
	// Mimecast (partial)
	"91.220.42.0/24",
	"207.211.30.0/24",
	"170.10.128.0/18",
	// Proofpoint (partial)
	"67.231.144.0/20",
	"148.163.128.0/17",
	"208.90.56.0/21",
	// Forcepoint / Websense (partial)
	"208.87.232.0/21",
	// Trend Micro (partial)
	"66.180.80.0/20",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ClassifyOpen decides whether a pixel hit should count as a human open.
func ClassifyOpen(userAgent, ip string, sentAt, now time.Time) OpenClassification {
	if IsBotUserAgent(userAgent) {
		return OpenClassification{IsBot: true, Reason: "user_agent"}
	}
	if !sentAt.IsZero() && !now.Before(sentAt) && now.Sub(sentAt) < MinHumanOpenDelay {
		return OpenClassification{IsBot: true, Reason: "too_fast"}
	}
	if IsScannerOrDatacenterIP(ip) {
		return OpenClassification{IsBot: true, Reason: "datacenter_ip"}
	}
	return OpenClassification{}
}

func IsBotUserAgent(ua string) bool {
	lower := strings.ToLower(strings.TrimSpace(ua))
	if lower == "" {
		// Empty UA is common for some corporate scanners.
		return true
	}
	for _, n := range botUserAgentNeedles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func IsScannerOrDatacenterIP(ipStr string) bool {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return false
	}
	if host, _, err := net.SplitHostPort(ipStr); err == nil {
		ipStr = host
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	// Loopback / private IPs on a tracking pixel usually mean misconfigured reverse proxy,
	// not a real recipient — don't treat as human engagement.
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range scannerCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
