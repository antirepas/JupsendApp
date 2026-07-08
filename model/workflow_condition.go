package model

import "fmt"

func DescribeConditionEngagement(cfg map[string]interface{}, graph WorkflowGraph) string {
	predicate, _ := cfg["predicate"].(string)
	params, _ := cfg["params"].(map[string]interface{})
	emailRef := describeConditionEmailRef(params, graph)
	predText := describeConditionPredicate(predicate, params)
	return fmt.Sprintf("If %s for %s", predText, emailRef)
}

func describeConditionEmailRef(params map[string]interface{}, graph WorkflowGraph) string {
	if params == nil {
		return "the most recent email sent in this workflow"
	}
	scope, _ := params["email_send_scope"].(string)
	if scope != "node" {
		return "the most recent email sent in this workflow"
	}
	key, _ := params["email_node_key"].(string)
	if key == "" {
		return "a selected email (not configured)"
	}
	for _, n := range graph.Nodes {
		if n.NodeKey == key && n.NodeType == "action_send_email" {
			return describeSendEmailNode(n)
		}
	}
	return fmt.Sprintf("email step %q", key)
}

func describeSendEmailNode(n WorkflowNode) string {
	if n.Label != "" && n.Label != "Send Email" && n.Label != "Send" {
		return fmt.Sprintf("%q", n.Label)
	}
	cfg := ParseNodeConfig(n.ConfigJSON)
	tid := int64(0)
	if v, ok := cfg["template_id"].(float64); ok {
		tid = int64(v)
	}
	name := fmt.Sprintf("template #%d", tid)
	if tid > 0 {
		if t, err := GetTemplate(tid); err == nil {
			name = t.Name
		}
	}
	if n.Label != "" && n.Label != "Send Email" && n.Label != "Send" {
		return fmt.Sprintf("%q (%s)", n.Label, name)
	}
	return name
}

func describeConditionPredicate(predicate string, params map[string]interface{}) string {
	switch predicate {
	case "has_opened":
		return "opened"
	case "has_not_opened":
		return describeNegativePredicate("did not open", params)
	case "has_replied":
		return "replied"
	case "has_not_replied":
		return describeNegativePredicate("did not reply", params)
	case "click_count_gte":
		min := 1
		if params != nil {
			if v, ok := params["min"].(float64); ok {
				min = int(v)
			}
		}
		if min <= 1 {
			return "clicked a link"
		}
		return fmt.Sprintf("clicked at least %d times", min)
	case "open_count_gte":
		min := 1
		if params != nil {
			if v, ok := params["min"].(float64); ok {
				min = int(v)
			}
		}
		if min <= 1 {
			return "opened"
		}
		return fmt.Sprintf("opened at least %d times", min)
	default:
		if predicate == "" {
			return "engagement matches"
		}
		return predicate
	}
}

func describeNegativePredicate(action string, params map[string]interface{}) string {
	days := 3
	if params != nil {
		if v, ok := params["wait_days"].(float64); ok && int(v) > 0 {
			days = int(v)
		}
	}
	if days == 1 {
		return fmt.Sprintf("still %s after 1 day", action)
	}
	return fmt.Sprintf("still %s after %d days", action, days)
}

func validateConditionEmailRef(n WorkflowNode, nodeTypes map[string]string) string {
	cfg := ParseNodeConfig(n.ConfigJSON)
	params, _ := cfg["params"].(map[string]interface{})
	if params == nil {
		return ""
	}
	scope, _ := params["email_send_scope"].(string)
	if scope != "node" {
		return ""
	}
	key, _ := params["email_node_key"].(string)
	if key == "" {
		return fmt.Sprintf("condition %s must link to a send-email step", n.NodeKey)
	}
	nt, ok := nodeTypes[key]
	if !ok {
		return fmt.Sprintf("condition %s references missing send step %s", n.NodeKey, key)
	}
	if nt != "action_send_email" {
		return fmt.Sprintf("condition %s must link to a send-email step, not %s", n.NodeKey, key)
	}
	return ""
}
