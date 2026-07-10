package util

import (
	"context"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"emailtracker.com/ai"
	"emailtracker.com/model"
)

// AICreditConsumer consumes one AI credit after a successful AI call.
type AICreditConsumer func(userID int64) bool

func aiCreditsAvailable(userID int64, check func(userID int64) bool) bool {
	if userID <= 0 || !ai.Enabled() {
		return false
	}
	if check != nil {
		return check(userID)
	}
	_, _, ok := model.AICreditsRemaining(userID)
	return ok
}

func chargeAICredit(userID int64, consume AICreditConsumer) {
	if consume != nil && userID > 0 {
		consume(userID)
	}
}

// ApplyAIFilters runs fit and summarize filters. Falls back on error or no credits.
func ApplyAIFilters(ctx context.Context, value string, ref VarRef, fullText string, tokenPos int, userID int64, consume AICreditConsumer, creditsCheck func(userID int64) bool, warnings *[]string) string {
	if userID <= 0 || !ai.Enabled() {
		if warnings != nil && ref.AIFit {
			*warnings = append(*warnings, "AI is not configured on the server.")
		}
		return value
	}
	if strings.TrimSpace(value) == "" {
		return value
	}
	result := value
	for _, f := range ref.Filters {
		if f.Name == "summarize" {
			if !aiCreditsAvailable(userID, creditsCheck) {
				log.Printf("template ai: credits exhausted for user %d", userID)
				appendAIWarning(warnings, "AI credits exhausted — summarize skipped for "+ref.Name+".")
				continue
			}
			maxWords := SummarizeWordCount(f.Arg)
			out, err := ai.CompleteTransform(ctx, ai.SummarizePrompt(maxWords), result)
			if err != nil {
				log.Printf("template ai summarize user=%d: %v", userID, err)
				appendAIWarning(warnings, fmt.Sprintf("Summarize failed for %s: %v", ref.Name, err))
				continue
			}
			out = strings.TrimSpace(out)
			if out == "" {
				appendAIWarning(warnings, fmt.Sprintf("Summarize returned empty text for %s.", ref.Name))
				continue
			}
			chargeAICredit(userID, consume)
			result = out
		}
	}
	if ref.AIFit {
		if !aiCreditsAvailable(userID, creditsCheck) {
			log.Printf("template ai: credits exhausted for user %d", userID)
			appendAIWarning(warnings, "AI credits exhausted — could not fit "+ref.Name+".")
			return result
		}
		context := extractFitContext(fullText, tokenPos, ref.Raw)
		userMsg := buildFitUserMessage(context, result)
		out, err := ai.CompleteTransform(ctx, ai.FitPrompt(), userMsg)
		if err != nil {
			log.Printf("template ai fit user=%d: %v", userID, err)
			appendAIWarning(warnings, fmt.Sprintf("AI fit failed for %s: %v", ref.Name, err))
			return result
		}
		out = strings.TrimSpace(out)
		if out == "" {
			log.Printf("template ai fit user=%d: empty response", userID)
			appendAIWarning(warnings, fmt.Sprintf("AI fit returned empty text for %s.", ref.Name))
			return result
		}
		chargeAICredit(userID, consume)
		result = out
	}
	return result
}

func appendAIWarning(warnings *[]string, msg string) {
	if warnings != nil && msg != "" {
		*warnings = append(*warnings, msg)
	}
}

func buildFitUserMessage(context, rawValue string) string {
	var b strings.Builder
	b.WriteString("Sentence context (___ marks the insertion point):\n")
	b.WriteString(context)
	b.WriteString("\n\nDescription to adapt:\n")
	b.WriteString(rawValue)
	if hint := fitGrammarHint(context); hint != "" {
		b.WriteString("\n\n")
		b.WriteString(hint)
	}
	return b.String()
}

func fitGrammarHint(context string) string {
	lower := strings.ToLower(context)
	switch {
	case strings.Contains(lower, " helps ___"):
		return "Grammar hint: after \"helps\", describe who they serve and what outcome they enable — e.g. \"therapists manage bookings and payments\". Do not start with \"B2B SaaS\" or repeat \"helping\"."
	case strings.Contains(lower, " noticed ___"):
		return "Grammar hint: after \"noticed\", use a natural clause — e.g. \"you are hiring\" or \"your team launched a new product\"."
	case strings.Contains(lower, " at ___"):
		return "Grammar hint: fit a short noun phrase that completes the preposition naturally."
	default:
		return ""
	}
}

func extractFitContext(text string, tokenPos int, rawToken string) string {
	idx := strings.Index(text[tokenPos:], rawToken)
	if idx < 0 {
		idx = 0
	} else {
		idx += tokenPos
	}
	// Map to plain offset approximately
	before := StripHTML(text[:idx])
	afterStart := idx + len(rawToken)
	after := ""
	if afterStart < len(text) {
		after = StripHTML(text[afterStart:])
	}
	const window = 120
	beforeRunes := []rune(before)
	if len(beforeRunes) > window {
		beforeRunes = beforeRunes[len(beforeRunes)-window:]
	}
	afterRunes := []rune(after)
	if len(afterRunes) > window {
		afterRunes = afterRunes[:window]
	}
	placeholder := "___"
	return string(beforeRunes) + placeholder + string(afterRunes)
}

// DefaultAICreditConsumer uses model.ConsumeAICredit.
var DefaultAICreditConsumer = func(userID int64) bool {
	_, _, ok := model.ConsumeAICredit(userID)
	return ok
}

// SummarizeWordCount parses summarize filter arg.
func SummarizeWordCount(arg string) int {
	n := 30
	if _, err := fmt.Sscanf(strings.TrimSpace(arg), "%d", &n); err != nil || n <= 0 {
		return 30
	}
	return n
}

// TruncateRunesForSubject strips HTML from values in subject line.
func TruncateRunesForSubject(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= max {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
