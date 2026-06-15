package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"emailtracker.com/model"
)

func EvaluateCondition(predicate string, params map[string]interface{}, inst model.WorkflowInstance, contactID int64) (bool, error) {
	switch predicate {
	case "has_opened":
		sendID, err := resolveScopeSendID(params, inst)
		if err != nil || sendID == 0 {
			return false, nil
		}
		return model.HasContactEventForSend(sendID, "OPEN")
	case "has_not_opened":
		ok, err := EvaluateCondition("has_opened", params, inst, contactID)
		return !ok, err
	case "open_count_gte":
		sendID, err := resolveScopeSendID(params, inst)
		if err != nil || sendID == 0 {
			return false, nil
		}
		min := intParam(params, "min", 1)
		n, _ := model.CountContactEventsForSend(sendID, "OPEN")
		return n >= min, nil
	case "click_count_gte":
		sendID, err := resolveScopeSendID(params, inst)
		if err != nil || sendID == 0 {
			return false, nil
		}
		min := intParam(params, "min", 1)
		n, _ := model.CountContactEventsForSend(sendID, "CLICK")
		return n >= min, nil
	case "clicked_url":
		sendID, err := resolveScopeSendID(params, inst)
		if err != nil || sendID == 0 {
			return false, nil
		}
		url, _ := params["url"].(string)
		return hasClickedURL(sendID, url), nil
	case "has_replied":
		sendID, err := resolveScopeSendID(params, inst)
		if err != nil || sendID == 0 {
			return false, nil
		}
		return model.HasContactEventForSend(sendID, "REPLY")
	case "has_not_replied":
		ok, err := EvaluateCondition("has_replied", params, inst, contactID)
		return !ok, err
	case "contact_var_equals":
		key, _ := params["key"].(string)
		want, _ := params["value"].(string)
		_, vars, err := model.GetContact(contactID)
		if err != nil {
			return false, err
		}
		for _, v := range vars {
			if v.Key == key && v.Value == want {
				return true, nil
			}
		}
		return false, nil
	case "variant_equals":
		want, _ := params["variant"].(string)
		ctx := model.GetInstanceContext(&inst)
		v, _ := ctx["variant"].(string)
		return strings.EqualFold(v, want), nil
	case "last_activity_older_than_days":
		days := intParam(params, "days", 3)
		sendID, _ := resolveScopeSendID(params, inst)
		if sendID == 0 {
			return true, nil
		}
		// simplified: no recent open/click in N days
		return !recentActivity(sendID, days), nil
	default:
		return false, fmt.Errorf("unknown predicate: %s", predicate)
	}
}

func resolveScopeSendID(params map[string]interface{}, inst model.WorkflowInstance) (int64, error) {
	scope, _ := params["email_send_scope"].(string)
	if scope == "last_in_workflow" || scope == "" {
		ctx := model.GetInstanceContext(&inst)
		if v, ok := ctx["last_send_id"]; ok {
			switch t := v.(type) {
			case float64:
				return int64(t), nil
			case int64:
				return t, nil
			case int:
				return int64(t), nil
			}
		}
		return model.GetLastSendIDForInstance(inst.ID)
	}
	return model.GetLastSendIDForInstance(inst.ID)
}

func intParam(params map[string]interface{}, key string, def int) int {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case string:
			n, _ := strconv.Atoi(t)
			if n > 0 {
				return n
			}
		}
	}
	return def
}

func hasClickedURL(sendID int64, url string) bool {
	n, _ := model.CountContactEventsForSend(sendID, "CLICK")
	if n == 0 {
		return false
	}
	if url == "" {
		return n > 0
	}
	// check contact events with metadata
	return n > 0
}

func recentActivity(sendID int64, days int) bool {
	// stub: if any open/click exists, consider recent
	n, _ := model.CountContactEventsForSend(sendID, "OPEN")
	if n > 0 {
		return true
	}
	n, _ = model.CountContactEventsForSend(sendID, "CLICK")
	return n > 0
}
