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

// AICreditConsumer consumes one AI credit; returns false if exhausted.
type AICreditConsumer func(userID int64) bool

// ApplyAIFilters runs fit and summarize filters. Falls back on error or no credits.
func ApplyAIFilters(ctx context.Context, value string, ref VarRef, fullText string, tokenPos int, userID int64, consume AICreditConsumer) string {
	if userID <= 0 {
		return value
	}
	result := value
	for _, f := range ref.Filters {
		if f.Name == "summarize" {
			if consume != nil && !consume(userID) {
				log.Printf("template ai: credits exhausted for user %d", userID)
				continue
			}
			maxWords := 30
			fmt.Sscanf(f.Arg, "%d", &maxWords)
			if maxWords <= 0 {
				maxWords = 30
			}
			out, err := ai.Complete(ctx, ai.SummarizePrompt(maxWords), result)
			if err != nil {
				log.Printf("template ai summarize: %v", err)
				continue
			}
			result = strings.TrimSpace(out)
		}
	}
	if ref.AIFit {
		if consume != nil && !consume(userID) {
			log.Printf("template ai: credits exhausted for user %d", userID)
			return result
		}
		context := extractFitContext(fullText, tokenPos, ref.Raw)
		userMsg := fmt.Sprintf("Sentence context:\n%s\n\nValue to insert:\n%s", context, result)
		out, err := ai.Complete(ctx, ai.FitPrompt(), userMsg)
		if err != nil {
			log.Printf("template ai fit: %v", err)
			return result
		}
		result = strings.TrimSpace(out)
	}
	return result
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
