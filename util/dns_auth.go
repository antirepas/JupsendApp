package util

import (
	"net"
	"strconv"
	"strings"
)

// DNSAuthCheck is one SPF/DKIM/DMARC/MX evaluation result for a sending domain.
type DNSAuthCheck struct {
	Name   string // MX, SPF, DKIM, DMARC
	Status string // pass, fail, warn, unknown
	Detail string
}

// Lookup hooks (overridable in tests).
var (
	lookupMX  = net.LookupMX
	lookupTXT = net.LookupTXT
)

var commonDKIMSelectors = []string{
	"google", "selector1", "selector2", "default", "s1", "s2", "k1", "mail",
}

// CheckDNSAuth resolves MX/TXT records for domain and evaluates common email auth records.
func CheckDNSAuth(domain string) []DNSAuthCheck {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "@")
	if domain == "" {
		return []DNSAuthCheck{{Name: "MX", Status: "fail", Detail: "No domain"}}
	}

	checks := make([]DNSAuthCheck, 0, 4)
	checks = append(checks, checkMX(domain))
	checks = append(checks, checkSPF(domain))
	checks = append(checks, checkDKIM(domain))
	checks = append(checks, checkDMARC(domain))
	return checks
}

func checkMX(domain string) DNSAuthCheck {
	mxs, err := lookupMX(domain)
	if err != nil || len(mxs) == 0 {
		detail := "No MX records found"
		if err != nil {
			detail = "MX lookup failed: " + err.Error()
		}
		return DNSAuthCheck{Name: "MX", Status: "fail", Detail: detail}
	}
	host := strings.TrimSuffix(mxs[0].Host, ".")
	detail := host
	if len(mxs) > 1 {
		detail += " (+" + strconv.Itoa(len(mxs)-1) + " more)"
	}
	return DNSAuthCheck{Name: "MX", Status: "pass", Detail: detail}
}

func checkSPF(domain string) DNSAuthCheck {
	txts, err := lookupTXT(domain)
	if err != nil {
		return DNSAuthCheck{Name: "SPF", Status: "fail", Detail: "TXT lookup failed: " + err.Error()}
	}
	for _, t := range txts {
		rec := strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(rec), "v=spf1") {
			return DNSAuthCheck{Name: "SPF", Status: "pass", Detail: truncate(rec, 120)}
		}
	}
	return DNSAuthCheck{Name: "SPF", Status: "fail", Detail: "No v=spf1 TXT record on " + domain}
}

func checkDMARC(domain string) DNSAuthCheck {
	name := "_dmarc." + domain
	txts, err := lookupTXT(name)
	if err != nil {
		return DNSAuthCheck{Name: "DMARC", Status: "fail", Detail: "Lookup failed for " + name}
	}
	for _, t := range txts {
		rec := strings.TrimSpace(t)
		if strings.Contains(strings.ToLower(rec), "v=dmarc1") {
			status := "pass"
			lower := strings.ToLower(rec)
			if strings.Contains(lower, "p=none") {
				status = "warn"
			}
			return DNSAuthCheck{Name: "DMARC", Status: status, Detail: truncate(rec, 120)}
		}
	}
	return DNSAuthCheck{Name: "DMARC", Status: "fail", Detail: "No v=DMARC1 record at " + name}
}

func checkDKIM(domain string) DNSAuthCheck {
	found := make([]string, 0, 2)
	for _, sel := range commonDKIMSelectors {
		name := sel + "._domainkey." + domain
		txts, err := lookupTXT(name)
		if err != nil || len(txts) == 0 {
			continue
		}
		for _, t := range txts {
			if strings.Contains(strings.ToLower(t), "v=dkim1") || strings.Contains(t, "p=") {
				found = append(found, sel)
				break
			}
		}
		if len(found) >= 2 {
			break
		}
	}
	if len(found) > 0 {
		return DNSAuthCheck{
			Name:   "DKIM",
			Status: "pass",
			Detail: "Found selector(s): " + strings.Join(found, ", "),
		}
	}
	return DNSAuthCheck{
		Name:   "DKIM",
		Status: "unknown",
		Detail: "No common DKIM selectors found — your provider may use a custom selector",
	}
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
