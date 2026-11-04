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
	"Europe/Athens",
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
	// Common city / metro labels from CRM exports
	"new york": "America/New_York", "nyc": "America/New_York", "new york city": "America/New_York",
	"los angeles": "America/Los_Angeles", "la": "America/Los_Angeles", "san francisco": "America/Los_Angeles",
	"chicago": "America/Chicago", "houston": "America/Chicago", "dallas": "America/Chicago",
	"denver": "America/Denver", "phoenix": "America/Phoenix", "seattle": "America/Los_Angeles",
	"toronto": "America/Toronto", "vancouver": "America/Vancouver", "montreal": "America/Toronto",
	"london": "Europe/London", "paris": "Europe/Paris", "berlin": "Europe/Berlin",
	"amsterdam": "Europe/Amsterdam", "madrid": "Europe/Madrid", "rome": "Europe/Rome",
	"athens": "Europe/Athens", "istanbul": "Europe/Istanbul", "stockholm": "Europe/Stockholm",
	"dublin": "Europe/Dublin", "zurich": "Europe/Zurich", "vienna": "Europe/Vienna",
	"sydney": "Australia/Sydney", "melbourne": "Australia/Melbourne", "tokyo": "Asia/Tokyo",
	"singapore": "Asia/Singapore", "dubai": "Asia/Dubai", "mumbai": "Asia/Kolkata", "delhi": "Asia/Kolkata",
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
	"gr": "Europe/Athens", "greece": "Europe/Athens",
	"tr": "Europe/Istanbul", "turkey": "Europe/Istanbul", "türkiye": "Europe/Istanbul",
	"pl": "Europe/Warsaw", "poland": "Europe/Warsaw",
	"cz": "Europe/Prague", "czech": "Europe/Prague", "czech republic": "Europe/Prague", "czechia": "Europe/Prague",
	"ro": "Europe/Bucharest", "romania": "Europe/Bucharest",
	"hu": "Europe/Budapest", "hungary": "Europe/Budapest",
	"lt": "Europe/Vilnius", "lithuania": "Europe/Vilnius",
	"lv": "Europe/Riga", "latvia": "Europe/Riga",
	"ee": "Europe/Tallinn", "estonia": "Europe/Tallinn",
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

// ResolveLocationValue maps a raw location cell (IANA TZ, alias, or country) to an IANA zone.
func ResolveLocationValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if tz := NormalizeTimezone(raw); tz != "" {
		return tz
	}
	return CountryToTimezone(raw)
}

// ResolveContactTimezone picks an IANA timezone from contact variables, else fallback.
// When preferredKey is set, that column is tried first (campaign location column).
func ResolveContactTimezone(vars []ContactVariables, fallbackIANA, preferredKey string) string {
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

	if pref := NormalizeVariableKey(preferredKey); pref != "" {
		if raw := byKey[pref]; raw != "" {
			if tz := ResolveLocationValue(raw); tz != "" {
				return tz
			}
		}
	}

	for _, key := range []string{"timezone", "tz", "time_zone"} {
		if raw := byKey[key]; raw != "" {
			if tz := NormalizeTimezone(raw); tz != "" {
				return tz
			}
		}
	}
	for _, key := range []string{"country", "country_code", "nation", "location", "region"} {
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
func SetCampaignLocalSend(campaignID, userID int64, enabled bool, minute int, fallbackTZ, locationKey string, sendDate *time.Time) error {
	minute = ClampLocalSendMinute(minute)
	if tz := NormalizeTimezone(fallbackTZ); tz != "" {
		fallbackTZ = tz
	} else {
		fallbackTZ = DefaultLocalSendFallbackTZ
	}
	locationKey = NormalizeVariableKey(locationKey)
	var dateArg interface{}
	if sendDate != nil && !sendDate.IsZero() {
		y, m, d := sendDate.Date()
		dateArg = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	res, err := db.Exec(`
		UPDATE campaigns
		SET local_send_enabled = ?, local_send_minute = ?, local_send_fallback_tz = ?,
		    local_send_location_key = ?, local_send_date = ?
		WHERE id = ? AND user_id = ? AND status NOT IN ('sent', 'sending')
	`, enabled, minute, fallbackTZ, locationKey, dateArg, campaignID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("campaign not found or already launched")
	}
	return nil
}

func contactLocation(camp Campaign, vars []ContactVariables) *time.Location {
	fallback := camp.LocalSendFallbackTZ
	if fallback == "" {
		fallback = DefaultLocalSendFallbackTZ
	}
	tzName := ResolveContactTimezone(vars, fallback, camp.LocalSendLocationKey)
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc, _ = time.LoadLocation(DefaultLocalSendFallbackTZ)
	}
	if loc == nil {
		return time.UTC
	}
	return loc
}

// LocalSendAtOnCivilDate returns hour:minute on the given civil date in loc.
func LocalSendAtOnCivilDate(civilDate time.Time, loc *time.Location, minute int) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	minute = ClampLocalSendMinute(minute)
	y, m, d := civilDate.Date()
	return time.Date(y, m, d, minute/60, minute%60, 0, 0, loc)
}

// CampaignLocalSendAt returns the send instant for a contact when local send is enabled.
// If LocalSendDate is set: that civil date at the preferred local clock time
// (may be in the past if launch ran late — workers treat past scheduled_at as send-now).
// Otherwise: next preferred local clock time after `after`.
func CampaignLocalSendAt(camp Campaign, vars []ContactVariables, after time.Time) (time.Time, bool) {
	if !camp.LocalSendEnabled {
		return time.Time{}, false
	}
	loc := contactLocation(camp, vars)
	minute := ClampLocalSendMinute(camp.LocalSendMinute)
	if camp.LocalSendDate != nil && !camp.LocalSendDate.IsZero() {
		return LocalSendAtOnCivilDate(*camp.LocalSendDate, loc, minute), true
	}
	return NextLocalSendAt(after, loc, minute/60, minute%60), true
}

// EarliestCampaignLocalSend returns the soonest absolute send time among contacts.
func EarliestCampaignLocalSend(camp Campaign, contactIDs []int64, after time.Time) (time.Time, error) {
	if !camp.LocalSendEnabled {
		return time.Time{}, fmt.Errorf("local send disabled")
	}
	var earliest time.Time
	found := false
	for _, id := range contactIDs {
		_, vars, err := GetContact(id)
		if err != nil {
			vars = nil
		}
		at, ok := CampaignLocalSendAt(camp, vars, after)
		if !ok {
			continue
		}
		if !found || at.Before(earliest) {
			earliest = at
			found = true
		}
	}
	if !found {
		return time.Time{}, fmt.Errorf("no contacts")
	}
	return earliest, nil
}

// ListCampaignLocationVariableKeys returns distinct contact variable keys on the campaign.
func ListCampaignLocationVariableKeys(campaignID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT cv.key
		FROM contact_variables cv
		INNER JOIN campaign_contacts cc ON cc.contact_id = cv.contact_id
		WHERE cc.campaign_id = ?
		  AND TRIM(COALESCE(cv.key, '')) <> ''
		  AND TRIM(COALESCE(cv.value, '')) <> ''
		ORDER BY cv.key
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		k := NormalizeVariableKey(raw)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys, nil
}

// SnapLocalSendWake advances a relative wait wake to the preferred local clock time.
// The wake is never earlier than floor (delay honored). Ignores LocalSendDate —
// that date is only for the initial campaign send; follow-ups use the clock time.
func SnapLocalSendWake(camp Campaign, contactID int64, floor time.Time) time.Time {
	if !camp.LocalSendEnabled {
		return floor
	}
	_, vars, err := GetContact(contactID)
	if err != nil {
		vars = nil
	}
	loc := contactLocation(camp, vars)
	minute := ClampLocalSendMinute(camp.LocalSendMinute)
	at := NextLocalSendAt(floor.Add(-time.Nanosecond), loc, minute/60, minute%60)
	if at.Before(floor) {
		return floor
	}
	return at
}
