package toolcall

import "strings"

func normalizeDSMLToolCallMarkup(text string) (string, bool) {
	if text == "" {
		return "", true
	}
	hasAliasLikeMarkup, _ := toolMarkupStylesOutsideIgnored(text)
	if !hasAliasLikeMarkup {
		return text, true
	}
	// Always normalize DSML aliases to canonical form, even when canonical
	// tags coexist. Models frequently mix DSML wrapper tags with canonical
	// inner tags (e.g., <｜tool_calls><invoke name="...">).
	return replaceDSMLToolMarkupOutsideIgnored(text), true
}

var dsmlToolMarkupAliases = []struct {
	from string
	to   string
}{
	{"<|dsml|tool_calls", "<tool_calls"},
	{"</|dsml|tool_calls>", "</tool_calls>"},
	{"<|dsml|invoke", "<invoke"},
	{"</|dsml|invoke>", "</invoke>"},
	{"<|dsml|parameter", "<parameter"},
	{"</|dsml|parameter>", "</parameter>"},
	{"<dsml|tool_calls", "<tool_calls"},
	{"</dsml|tool_calls>", "</tool_calls>"},
	{"<dsml|invoke", "<invoke"},
	{"</dsml|invoke>", "</invoke>"},
	{"<dsml|parameter", "<parameter"},
	{"</dsml|parameter>", "</parameter>"},
	{"<|tool_calls", "<tool_calls"},
	{"</|tool_calls>", "</tool_calls>"},
	{"<|invoke", "<invoke"},
	{"</|invoke>", "</invoke>"},
	{"<|parameter", "<parameter"},
	{"</|parameter>", "</parameter>"},
	{"<｜tool_calls", "<tool_calls"},
	{"</｜tool_calls>", "</tool_calls>"},
	{"<｜invoke", "<invoke"},
	{"</｜invoke>", "</invoke>"},
	{"<｜parameter", "<parameter"},
	{"</｜parameter>", "</parameter>"},
}

var canonicalToolMarkupPrefixes = []string{
	"<tool_calls",
	"</tool_calls>",
	"<invoke",
	"</invoke>",
	"<parameter",
	"</parameter>",
}

func toolMarkupStylesOutsideIgnored(text string) (hasDSML, hasCanonical bool) {
	lower := strings.ToLower(text)
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return hasDSML, hasCanonical
		}
		if advanced {
			i = next
			continue
		}
		if hasPrefixAt(lower, i, canonicalToolMarkupPrefixes) {
			hasCanonical = true
		}
		for _, alias := range dsmlToolMarkupAliases {
			if strings.HasPrefix(lower[i:], alias.from) {
				hasDSML = true
				break
			}
		}
		if hasDSML && hasCanonical {
			return true, true
		}
		i++
	}
	return hasDSML, hasCanonical
}

func replaceDSMLToolMarkupOutsideIgnored(text string) string {
	lower := strings.ToLower(text)
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			b.WriteString(text[i:])
			break
		}
		if advanced {
			b.WriteString(text[i:next])
			i = next
			continue
		}
		replaced := false
		for _, alias := range dsmlToolMarkupAliases {
			if strings.HasPrefix(lower[i:], alias.from) {
				b.WriteString(alias.to)
				i += len(alias.from)
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

func hasPrefixAt(text string, idx int, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text[idx:], prefix) {
			return true
		}
	}
	return false
}
