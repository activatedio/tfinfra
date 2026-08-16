package tf

import (
	"strings"
	"unicode"
)

// toSnake converts a Go identifier to snake_case, treating consecutive
// capitals as one word ("APIKey" → "api_key").
func toSnake(s string) string {

	runes := []rune(s)
	var b strings.Builder

	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && unicode.IsLower(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}

// snakeToCamel converts snake_case to an exported Go identifier
// ("store_id" → "StoreId").
func snakeToCamel(s string) string {

	var b strings.Builder

	for _, part := range strings.Split(s, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	return b.String()
}
