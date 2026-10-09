package util

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// NormalizeQuillHTML converts Quill 2 editor markup into email-safe HTML.
// Quill stores both bullets and numbers as <ol><li data-list="…"> with empty
// <span class="ql-ui"> markers that rely on editor CSS — email clients render
// that as broken/missing lists. This turns them into real <ul>/<ol> lists.
func NormalizeQuillHTML(input string) string {
	if input == "" {
		return input
	}
	lower := strings.ToLower(input)
	if !strings.Contains(input, "data-list") && !strings.Contains(input, "ql-ui") && !strings.Contains(lower, "ql-") {
		return input
	}

	ctx := &html.Node{Type: html.ElementNode, Data: atom.Div.String(), DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(input), ctx)
	if err != nil || len(nodes) == 0 {
		return input
	}

	wrapper := &html.Node{Type: html.ElementNode, Data: "div"}
	for _, n := range nodes {
		wrapper.AppendChild(n)
	}
	normalizeQuillNode(wrapper)

	var out bytes.Buffer
	for c := wrapper.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&out, c); err != nil {
			return input
		}
	}
	return out.String()
}

func normalizeQuillNode(n *html.Node) {
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "ol" || tag == "ul" {
			if news := rewriteQuillList(n); news != nil {
				for _, nl := range news {
					stripQuillClasses(nl)
					for c := nl.FirstChild; c != nil; {
						next := c.NextSibling
						normalizeQuillNode(c)
						c = next
					}
				}
				return
			}
		}
		if tag == "span" && hasClass(n, "ql-ui") {
			removeNode(n)
			return
		}
		stripQuillClasses(n)
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		normalizeQuillNode(c)
		c = next
	}
}

// rewriteQuillList replaces a Quill data-list ol/ul with semantic lists.
// Returns the new list nodes, or nil if no rewrite was needed.
func rewriteQuillList(list *html.Node) []*html.Node {
	if list == nil || list.Parent == nil {
		return nil
	}
	items := childElements(list, "li")
	if len(items) == 0 {
		return nil
	}
	hasDataList := false
	for _, li := range items {
		if attr(li, "data-list") != "" {
			hasDataList = true
			break
		}
	}
	if !hasDataList {
		return nil
	}

	var groups [][]*html.Node
	var types []string
	curType := ""
	var cur []*html.Node
	for _, li := range items {
		t := strings.ToLower(strings.TrimSpace(attr(li, "data-list")))
		if t == "" {
			t = "ordered"
		}
		if t == "checked" || t == "unchecked" {
			t = "bullet"
		}
		if len(cur) == 0 || t != curType {
			if len(cur) > 0 {
				groups = append(groups, cur)
				types = append(types, curType)
			}
			curType = t
			cur = []*html.Node{li}
			continue
		}
		cur = append(cur, li)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
		types = append(types, curType)
	}

	parent := list.Parent
	ref := list
	news := make([]*html.Node, 0, len(groups))
	for i, group := range groups {
		tag := "ol"
		if types[i] == "bullet" {
			tag = "ul"
		}
		newList := &html.Node{Type: html.ElementNode, Data: tag}
		for _, li := range group {
			clean := cloneElementShallow(li)
			removeAttr(clean, "data-list")
			stripQuillClasses(clean)
			moveChildren(li, clean)
			stripQuillUISpans(clean)
			newList.AppendChild(clean)
		}
		parent.InsertBefore(newList, ref)
		news = append(news, newList)
	}
	parent.RemoveChild(list)
	return news
}

func stripQuillUISpans(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, "span") && hasClass(c, "ql-ui") {
			n.RemoveChild(c)
		} else {
			stripQuillUISpans(c)
		}
		c = next
	}
}

func stripQuillClasses(n *html.Node) {
	if n.Type != html.ElementNode {
		return
	}
	for i := range n.Attr {
		if !strings.EqualFold(n.Attr[i].Key, "class") {
			continue
		}
		parts := strings.Fields(n.Attr[i].Val)
		kept := parts[:0]
		for _, p := range parts {
			if strings.HasPrefix(strings.ToLower(p), "ql-") {
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) == 0 {
			removeAttr(n, "class")
		} else {
			n.Attr[i].Val = strings.Join(kept, " ")
		}
		return
	}
}

func childElements(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, tag) {
			out = append(out, c)
		}
	}
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func removeAttr(n *html.Node, key string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			continue
		}
		out = append(out, a)
	}
	n.Attr = out
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if strings.EqualFold(c, class) {
			return true
		}
	}
	return false
}

func removeNode(n *html.Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

func cloneElementShallow(n *html.Node) *html.Node {
	out := &html.Node{Type: html.ElementNode, Data: n.Data}
	out.Attr = append([]html.Attribute(nil), n.Attr...)
	return out
}

func moveChildren(from, to *html.Node) {
	for c := from.FirstChild; c != nil; {
		next := c.NextSibling
		from.RemoveChild(c)
		to.AppendChild(c)
		c = next
	}
}
