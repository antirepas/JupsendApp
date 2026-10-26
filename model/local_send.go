package model

import (
	"fmt"
	"strings"
	"time"

	"emailtracker.com/db"
)

const DefaultLocalSendFallbackTZ = "America/New_York"
const DefaultLocalSendMinute = 9 * 60 // 09:00

// CommonFallbackTimezones is a short curated list for campaign UI selects.
var CommonFallbackTimezones = []string{
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"America/Sao_Paulo",
	"Europe/London",
	"Europe/Paris",
	"Europe/Berlin",
	"Europe/Amsterdam",
	"Asia/Dubai",
	"Asia/Kolkata",
	"Asia/Singapore",
	"Asia/Tokyo",
	"Australia/Sydney",
	"Pacific/Auckland",
	"UTC",
}

var timezoneAliases = map[string]string{
	"est": "America/New_York", "edt": "America/New_York", "et": "America/New_York",
	"eastern": "America/New_York", "us/eastern": "America/New_York",
	"cst": "America/Chicago", "cdt": "America/Chicago", "ct": "America/Chicago",
	"central": "America/Chicago", "us/central": "America/Chicago",
	"mst": "America/Denver", "mdt": "America/Denver", "mt": "America/Denver",
	"mountain": "America/Denver", "us/mountain": "America/Denver",
	"pst": "America/Los_Angeles", "pdt": "America/Los_Angeles", "pt": "America/Los_Angeles",
	"pacific": "America/Los_Angeles", "us/pacific": "America/Los_Angeles",
	"gmt": "Europe/London", "bst": "Europe/London", "uk": "Europe/London",
	"cet": "Europe/Paris", "cest": "Europe/Paris",
	"ist": "Asia/Kolkata", "jst": "Asia/Tokyo", "aest": "Australia/Sydney", "aedt": "Australia/Sydney",
}

var countryToTZ = map[string]string{
	"us": "America/New_York", "usa": "America/New_York", "united states": "America/New_York",
	"united states of america": "America/New_York", "america": "America/New_York",
	"ca": "America/Toronto", "canada": "America/Toronto",
	"mx": "America/Mexico_City", "mexico": "America/Mexico_City",
	"br": "America/Sao_Paulo", "brazil": "America/Sao_Paulo",
	"gb": "Europe/London", "uk": "Europe/London", "united kingdom": "Europe/London",
	"england": "Europe/London", "great britain": "Europe/London",
	"ie": "Europe/Dublin", "ireland": "Europe/Dublin",
	"fr": "Europe/Paris", "france": "Europe/Paris",
	"de": "Europe/Berlin", "germany": "Europe/Berlin", "deutschland": "Europe/Berlin",
	"nl": "Europe/Amsterdam", "netherlands": "Europe/Amsterdam", "holland": "Europe/Amsterdam",
	"be": "Europe/Brussels", "belgium": "Europe/Brussels",
	"es": "Europe/Madrid", "spain": "Europe/Madrid",
	"it": "Europe/Rome", "italy": "Europe/Rome",
	"pt": "Europe/Lisbon", "portugal": "Europe/Lisbon",
	"ch": "Europe/Zurich", "switzerland": "Europe/Zurich",
	"at": "Europe/Vienna", "austria": "Europe/Vienna",
	"se": "Europe/Stockholm", "sweden": "Europe/Stockholm",
	"no": "Europe/Oslo", "norway": "Europe/Oslo",
	"dk": "Europe/Copenhagen", "denmark": "Europe/Copenhagen",
	"fi": "Europe/Helsinki", "finland": "Europe/Helsinki",
	"pl": "Europe/Warsaw", "poland": "Europe/Warsaw",
	"cz": "Europe/Prague", "czech": "Europe/Prague", "czech republic": "Europe/Prague", "czechia": "Europe/Prague",
	"au": "Australia/Sydney", "australia": "Australia/Sydney",
	"nz": "Pacific/Auckland", "new zealand": "Pacific/Auckland",
	"in": "Asia/Kolkata", "india": "Asia/Kolkata",
	"sg": "Asia/Singapore", "singapore": "Asia/Singapore",
	"jp": "Asia/Tokyo", "japan": "Asia/Tokyo",
	"kr": "Asia/Seoul", "south korea": "Asia/Seoul", "korea": "Asia/Seoul",
	"cn": "Asia/Shanghai", "china": "Asia/Shanghai",
	"hk": "Asia/Hong_Kong", "hong kong": "Asia/Hong_Kong",
	"ae": "Asia/Dubai", "uae": "Asia/Dubai", "united arab emirates": "Asia/Dubai",
	"il": "Asia/Jerusalem", "israel": "Asia/Jerusalem",
	"za": "Africa/Johannesburg", "south africa": "Africa/Johannesburg",
	"ng": "Africa/Lagos", "nigeria": "Africa/Lagos",
	"ke": "Africa/Nairobi", "kenya": "Africa/Nairobi",
	"ph": "Asia/Manila", "philippines": "Asia/Manila",
	"id": "Asia/Jakarta", "indonesia": "Asia/Jakarta",
	"my": "Asia/Kuala_Lumpur", "malaysia": "Asia/Kuala_Lumpur",
	"th": "Asia/Bangkok", "thailand": "Asia/Bangkok",
	"vn": "Asia/Ho_Chi_Minh", "vietnam": "Asia/Ho_Chi_Minh",
	"ar": "America/Argentina/Buenos_Aires", "argentina": "America/Argentina/Buenos_Aires",
	"cl": "America/Santiago", "chile": "America/Santiago",
	"co": "America/Bogota", "colombia": "America/Bogota",
	"pe": "America/Lima", "peru": "America/Lima",
}

// ResolveContactTimezone picks an IANA timezone from contact variables, else fallback.
func ResolveContactTimezone(vars []ContactVariables, fallbackIANA string) string {
	fallback := NormalizeTimezone(fallbackIANA)
	if fallback == "" {
		fallback = DefaultLocalSendFallbackTZ
	}

	byKey := map[string]string{}
	for _, v := range vars {
		k := NormalizeVariableKey(v.Key)
		if k == "" {
			continue
		}
		byKey[k] = strings.TrimSpace(v.Value)
	}

	for _, key := range []string{"timezone", "tz", "time_zone"} {
		if raw := byKey[key]; raw != "" {
			if tz := NormalizeTimezone(raw); tz != "" {
				return tz
			}
		}
	}
	for _, key := range []string{"country", "country_code", "nation"} {
		if raw := byKey[key]; raw != "" {
			if tz := CountryToTimezone(raw); tz != "" {
				return tz
			}
		}
	}
	return fallback
}

// NormalizeTimezone accepts IANA names or common aliases; empty if unknown.
func NormalizeTimezone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if alias, ok := timezoneAliases[strings.ToLower(s)]; ok {
		s = alias
	}
	s = strings.ReplaceAll(s, " ", "_")
	if _, err := time.LoadLocation(s); err != nil {
		return ""
	}
	return s
}

// CountryToTimezone maps a country name or ISO code to a representative IANA zone.
func CountryToTimezone(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	key = strings.TrimSuffix(key, ".")
	if tz, ok := countryToTZ[key]; ok {
		return tz
	}
	return ""
}

// NextLocalSendAt returns the first HH:MM in loc that is strictly after `after`.
func NextLocalSendAt(after time.Time, loc *time.Location, hour, min int) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	if hour < 0 || hour > 23 {
		hour = 9
	}
	if min < 0 || min > 59 {
		min = 0
	}
	after = after.In(loc)
	candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, min, 0, 0, loc)
	if !candidate.After(after) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

// LocalSendTimeString formats minutes-from-midnight as HH:MM for form inputs.
func LocalSendTimeString(minute int) string {
	minute = ClampLocalSendMinute(minute)
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// ParseLocalSendTime parses "HH:MM" or "H:MM" into minutes from midnight.
func ParseLocalSendTime(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0, false
	}
	h := atoiLocalSend(parts[0])
	m := atoiLocalSend(parts[1])
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func atoiLocalSend(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ClampLocalSendMinute keeps minute-of-day in range.
func ClampLocalSendMinute(m int) int {
	if m < 0 || m >= 24*60 {
		return DefaultLocalSendMinute
	}
	return m
}

// SetCampaignLocalSend updates preferred local send settings (draft campaigns).
func SetCampaignLocalSend(campaignID, userID int64, enabled bool, minute int, fallbackTZ string) error {
	minute = ClampLocalSendMinute(minute)
	if tz := NormalizeTimezone(fallbackTZ); tz != "" {
		fallbackTZ = tz
	} else {
		fallbackTZ = DefaultLocalSendFallbackTZ
	}
	res, err := db.Exec(`
		UPDATE campaigns
		SET local_send_enabled = ?, local_send_minute = ?, local_send_fallback_tz = ?
		WHERE id = ? AND user_id = ? AND status NOT IN ('sent', 'sending')
	`, enabled, minute, fallbackTZ, campaignID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("campaign not found or already launched")
	}
	return nil
}

// CampaignLocalSendAt returns the next preferred local send time when enabled.
func CampaignLocalSendAt(camp Campaign, vars []ContactVariables, after time.Time) (time.Time, bool) {
	if !camp.LocalSendEnabled {
		return time.Time{}, false
	}
	fallback := camp.LocalSendFallbackTZ
	if fallback == "" {
		fallback = DefaultLocalSendFallbackTZ
	}
	tzName := ResolveContactTimezone(vars, fallback)
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc, _ = time.LoadLocation(DefaultLocalSendFallbackTZ)
	}
	if loc == nil {
		loc = time.UTC
	}
	minute := ClampLocalSendMinute(camp.LocalSendMinute)
	return NextLocalSendAt(after, loc, minute/60, minute%60), true
}

// SnapLocalSendWake advances a relative wait wake to the preferred local clock time.
// The wake is never earlier than floor (delay honored).
func SnapLocalSendWake(camp Campaign, contactID int64, floor time.Time) time.Time {
	if !camp.LocalSendEnabled {
		return floor
	}
	_, vars, err := GetContact(contactID)
	if err != nil {
		vars = nil
	}
	at, ok := CampaignLocalSendAt(camp, vars, floor.Add(-time.Nanosecond))
	if !ok {
		return floor
	}
	if at.Before(floor) {
		return floor
	}
	return at
}
