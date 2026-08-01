package util

import (
	"context"
	"fmt"
	"html"
	"strings"

	"emailtracker.com/ai"
	"emailtracker.com/config"
	"emailtracker.com/model"
)

func lookupContactVar(varMap map[string]string, name string) string {
	if v, ok := varMap[name]; ok {
		return v
	}
	for k, v := range varMap {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// RenderOptions controls template rendering behavior.
type RenderOptions struct {
	UserID         int64
	ForPreview     bool
	UseAI          bool
	BodyMode       bool // true for HTML body, false for plain subject
	Ctx            context.Context
	ConsumeAI      AICreditConsumer
	AICreditsCheck func(userID int64) bool // optional override (tests)
	AIWarnings     *[]string               // optional collector for preview UI
}

// RenderResult holds rendered text and any missing required variables.
type RenderResult struct {
	Text            string
	MissingRequired []string
}

// RenderEmail renders subject and body for a contact.
func RenderEmail(subject, body string, contactVars []model.ContactVariables, opts RenderOptions) (renderedSubject, renderedBody string, missing []string, err error) {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.ConsumeAI == nil && opts.UserID > 0 {
		opts.ConsumeAI = DefaultAICreditConsumer
	}
	subjOpts := opts
	subjOpts.BodyMode = false
	bodyOpts := opts
	bodyOpts.BodyMode = true

	subjRes, err := RenderTemplate(subject, contactVars, subjOpts)
	if err != nil {
		return "", "", nil, err
	}
	bodyRes, err := RenderTemplate(body, contactVars, bodyOpts)
	if err != nil {
		return "", "", nil, err
	}
	missing = uniqueStrings(append(subjRes.MissingRequired, bodyRes.MissingRequired...))
	return subjRes.Text, bodyRes.Text, missing, nil
}

// RenderTemplate renders a single template string with filters and blocks.
func RenderTemplate(tBody string, contactVars []model.ContactVariables, opts RenderOptions) (RenderResult, error) {
	config.Reload()
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.ConsumeAI == nil && opts.UserID > 0 {
		opts.ConsumeAI = DefaultAICreditConsumer
	}

	varMap := make(map[string]string)
	for _, cv := range contactVars {
		varMap[cv.Key] = cv.Value
	}

	text := tBody
	refs := ParseVarRefs(text)

	// Process {% if %} blocks (innermost first by replacing from end)
	blocks := ParseIfBlocks(text)
	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		if ValueForIfBlock(b.VarName, varMap, refs) {
			text = text[:b.TokenPos] + b.Inner + text[b.TokenPos+len(b.Raw):]
		} else {
			text = text[:b.TokenPos] + text[b.TokenPos+len(b.Raw):]
		}
	}

	// Re-parse refs after block processing
	refs = ParseVarRefs(text)

	var missing []string
	runAI := ai.Enabled() && opts.UserID > 0 && (!opts.ForPreview || opts.UseAI)

	// Replace each var ref from end to start to preserve positions
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		raw := lookupContactVar(varMap, ref.Name)
		value, required, rawHTML := ApplyFilters(raw, ref.Filters, opts.BodyMode)

		if required && isEmptyValue(value) {
			missing = append(missing, ref.Name)
			value = ""
		}

		if runAI && (ref.AIFit || hasFilter(ref.Filters, "summarize")) {
			value = ApplyAIFilters(opts.Ctx, value, ref, text, ref.TokenPos, opts.UserID, opts.ConsumeAI, opts.AICreditsCheck, opts.AIWarnings)
		}

		if !opts.BodyMode {
			value = StripHTML(value)
		} else if !rawHTML {
			value = html.EscapeString(value)
		}

		text = text[:ref.TokenPos] + value + text[ref.TokenPos+len(ref.Raw):]
	}

	return RenderResult{Text: text, MissingRequired: uniqueStrings(missing)}, nil
}

func hasFilter(filters []Filter, name string) bool {
	for _, f := range filters {
		if f.Name == name {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// LegacyRenderTemplate is kept for minimal backward compat — uses plain substitution only.
// Deprecated: use RenderTemplate with RenderOptions instead.
func LegacyRenderTemplate(tBody string, contactVars []model.ContactVariables, _ string) (string, error) {
	res, err := RenderTemplate(tBody, contactVars, RenderOptions{})
	return res.Text, err
}

// InjectTrackingPixel appends a 1x1 open-tracking image inside the HTML body.
func InjectTrackingPixel(body, trackId string) string {
	return InjectTrackingPixelWithBase(body, trackId, config.BaseURL)
}

func InjectTrackingPixelWithBase(body, trackId, baseURL string) string {
	pixelURL := TrackOpenURL(baseURL, trackId)

	pixel := fmt.Sprintf(
		`<img src="%s" width="1" height="1" alt="" border="0" style="display:block;width:1px;height:1px;border:0;margin:0;padding:0;line-height:1;" />`,
		pixelURL,
	)
	pixel += fmt.Sprintf(
		`<div aria-hidden="true" style="width:1px;height:1px;max-height:0;overflow:hidden;line-height:1px;background-image:url('%s');background-repeat:no-repeat;"></div>`,
		pixelURL,
	)
	pixel += fmt.Sprintf(
		`<div style="display:none;max-height:0;overflow:hidden;"><img src="%s" width="1" height="1" alt="" /></div>`,
		pixelURL,
	)

	lower := strings.ToLower(body)
	if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
		return body[:idx] + pixel + body[idx:]
	}
	return body + pixel
}

func RewriteLinks(htmlBody string, emailSendId int64) string {
	return RewriteLinksWithBase(htmlBody, emailSendId, config.BaseURL)
}

func RewriteLinksWithBase(htmlBody string, emailSendId int64, baseURL string) string {
	htmlBody = AutolinkBareURLs(htmlBody)

	return hrefAttrRe.ReplaceAllStringFunc(htmlBody, func(match string) string {
		sub := hrefAttrRe.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		originalURL := sub[2]
		if originalURL == "" {
			originalURL = sub[3]
		}
		if shouldSkipLinkTracking(originalURL) {
			return match
		}
		dest, ok := normalizeTrackableURL(originalURL)
		if !ok {
			return match
		}

		linkTrackingID := GenerateLinkTrackingID()
		_, err := model.SaveTrackLink(emailSendId, linkTrackingID, dest)
		if err != nil {
			return match
		}

		return fmt.Sprintf(`href="%s"`, TrackClickURL(baseURL, linkTrackingID))
	})
}
