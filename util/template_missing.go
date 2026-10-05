package util

import (
	"strings"

	"emailtracker.com/model"
)

// MissingContactVarsForTemplates returns contact variable keys used in the templates
// that are empty for this contact and have no |default filter.
// Used to block campaign/workflow sends that would personalize to blank values.
func MissingContactVarsForTemplates(contactVars []model.ContactVariables, parts ...string) []string {
	varMap := make(map[string]string, len(contactVars))
	for _, cv := range contactVars {
		varMap[cv.Key] = cv.Value
	}

	defaults := map[string]bool{}
	needed := map[string]string{} // lower -> display name
	for _, part := range parts {
		for _, ref := range ParseVarRefs(part) {
			if ref.Mailbox {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(ref.Name))
			if key == "" {
				continue
			}
			if _, ok := needed[key]; !ok {
				needed[key] = ref.Name
			}
			for _, f := range ref.Filters {
				if strings.EqualFold(f.Name, "default") && strings.TrimSpace(f.Arg) != "" {
					defaults[key] = true
				}
			}
		}
	}

	var missing []string
	for key, display := range needed {
		if defaults[key] {
			continue
		}
		if strings.TrimSpace(lookupContactVar(varMap, display)) != "" {
			continue
		}
		missing = append(missing, display)
	}
	return uniqueStrings(missing)
}
