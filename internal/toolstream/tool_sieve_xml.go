package toolstream

import (
	"ds2api/internal/toolcall"
	"regexp"
	"strings"
)

// --- XML tool call support for the streaming sieve ---

//nolint:unused // kept as explicit tag inventory for future XML sieve refinements.
var xmlToolCallClosingTags = []string{"</tool_calls>", "</|dsml|tool_calls>", "</dsml|tool_calls>", "</｜tool_calls>", "</|tool_calls>"}
var xmlToolCallOpeningTags = []string{
	"<tool_calls", "<invoke",
	"<|dsml|tool_calls", "<|dsml|invoke",
	"<dsml|tool_calls", "<dsml|invoke",
	"<｜tool_calls", "<｜invoke",
	"<|tool_calls", "<|invoke",
}

// xmlToolCallTagPairs maps each opening tag to its expected closing tag.
// Order matters: longer/wrapper tags must be checked first.
var xmlToolCallTagPairs = []struct{ open, close string }{
	{"<|dsml|tool_calls", "</|dsml|tool_calls>"},
	{"<dsml|tool_calls", "</dsml|tool_calls>"},
	{"<｜tool_calls", "</｜tool_calls>"},
	{"<|tool_calls", "</|tool_calls>"},
	{"<tool_calls", "</tool_calls>"},
}

// xmlToolCallBlockPattern matches a complete canonical XML tool call block.
//
//nolint:unused // reserved for future fast-path XML block detection.
var xmlToolCallBlockPattern = regexp.MustCompile(`(?is)((?:<tool_calls\b|<\|dsml\|tool_calls\b)[^>]*>\s*(?:.*?)\s*(?:</tool_calls>|</\|dsml\|tool_calls>))`)

// xmlToolTagsToDetect is the set of XML tag prefixes used by findToolSegmentStart.
var xmlToolTagsToDetect = []string{
	"<|dsml|tool_calls>", "<|dsml|tool_calls\n", "<|dsml|tool_calls ",
	"<|dsml|invoke ", "<|dsml|invoke\n", "<|dsml|invoke\t", "<|dsml|invoke\r",
	"<dsml|tool_calls>", "<dsml|tool_calls\n", "<dsml|tool_calls ",
	"<dsml|invoke ", "<dsml|invoke\n", "<dsml|invoke\t", "<dsml|invoke\r",
	"<｜tool_calls>", "<｜tool_calls\n", "<｜tool_calls ",
	"<｜invoke ", "<｜invoke\n", "<｜invoke\t", "<｜invoke\r",
	"<|tool_calls>", "<|tool_calls\n", "<|tool_calls ",
	"<|invoke ", "<|invoke\n", "<|invoke\t", "<|invoke\r",
	"<tool_calls>", "<tool_calls\n", "<tool_calls ", "<invoke ", "<invoke\n", "<invoke\t", "<invoke\r",
}

// consumeXMLToolCapture tries to extract complete XML tool call blocks from captured text.
func consumeXMLToolCapture(captured string, toolNames []string) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	lower := strings.ToLower(captured)
	anyOpenFound := false
	type candidate struct {
		start  int
		prefix string
		calls  []toolcall.ParsedToolCall
		suffix string
	}
	type rejectedBlock struct {
		start  int
		prefix string
		suffix string
	}
	var best *candidate
	var rejected *rejectedBlock

	// Scan every wrapper occurrence. Prose can mention a wrapper tag before the
	// actual tool block, including the same variant as the real block.
	for _, pair := range xmlToolCallTagPairs {
		searchFrom := 0
		for searchFrom < len(lower) {
			openIdx := findXMLOpenOutsideCDATA(captured, pair.open, searchFrom)
			if openIdx < 0 {
				break
			}
			// Find the matching closing tag outside CDATA. Long write-file tool
			// calls often contain XML examples in CDATA, including </tool_calls>.
			closeIdx := findMatchingXMLToolWrapperClose(captured, pair.open, pair.close, openIdx)
			if closeIdx < 0 {
				anyOpenFound = true
				searchFrom = openIdx + len(pair.open)
				continue
			}
			closeEnd := closeIdx + len(pair.close)

			xmlBlock := captured[openIdx:closeEnd]
			prefixPart := captured[:openIdx]
			suffixPart := captured[closeEnd:]
			parsed := toolcall.ParseToolCalls(xmlBlock, toolNames)
			if len(parsed) > 0 {
				prefixPart, suffixPart = trimWrappingJSONFence(prefixPart, suffixPart)
				if best == nil || openIdx < best.start {
					best = &candidate{start: openIdx, prefix: prefixPart, calls: parsed, suffix: suffixPart}
				}
				break
			}
			if rejected == nil || openIdx < rejected.start {
				rejected = &rejectedBlock{start: openIdx, prefix: prefixPart + xmlBlock, suffix: suffixPart}
			}
			searchFrom = openIdx + len(pair.open)
		}
	}
	if best != nil {
		return best.prefix, best.calls, best.suffix, true
	}
	if anyOpenFound {
		// At least one opening tag was found but none had a matching close tag.
		// Keep buffering until a closing tag arrives.
		return "", nil, "", false
	}
	if rejected != nil {
		// If this block failed to become a tool call, pass it through as text.
		return rejected.prefix, nil, rejected.suffix, true
	}
	if !containsAnyToolCallWrapper(lower) {
		invokeIdx, dsml := firstInvokeIndex(lower)
		closeTag := "</tool_calls>"
		openWrapper := "<tool_calls>"
		if dsml {
			closeTag = "</|dsml|tool_calls>"
			openWrapper = "<|DSML|tool_calls>"
		}
		closeIdx := findXMLCloseOutsideCDATA(captured, closeTag, invokeIdx)
		if invokeIdx >= 0 && closeIdx > invokeIdx {
			closeEnd := closeIdx + len(closeTag)
			xmlBlock := openWrapper + captured[invokeIdx:closeIdx] + closeTag
			prefixPart := captured[:invokeIdx]
			suffixPart := captured[closeEnd:]
			parsed := toolcall.ParseToolCalls(xmlBlock, toolNames)
			if len(parsed) > 0 {
				prefixPart, suffixPart = trimWrappingJSONFence(prefixPart, suffixPart)
				return prefixPart, parsed, suffixPart, true
			}
			return prefixPart + captured[invokeIdx:closeEnd], nil, suffixPart, true
		}
	}
	return "", nil, "", false
}

func findMatchingXMLToolWrapperClose(s, openTag, closeTag string, openIdx int) int {
	if s == "" || openTag == "" || closeTag == "" || openIdx < 0 {
		return -1
	}
	lower := strings.ToLower(s)
	openTarget := strings.ToLower(openTag)
	closeTarget := strings.ToLower(closeTag)
	depth := 1
	for i := openIdx + len(openTarget); i < len(s); {
		switch {
		case strings.HasPrefix(lower[i:], "<![cdata["):
			end := strings.Index(lower[i+len("<![cdata["):], "]]>")
			if end < 0 {
				return -1
			}
			i += len("<![cdata[") + end + len("]]>")
		case strings.HasPrefix(lower[i:], "<!--"):
			end := strings.Index(lower[i+len("<!--"):], "-->")
			if end < 0 {
				return -1
			}
			i += len("<!--") + end + len("-->")
		case strings.HasPrefix(lower[i:], closeTarget):
			depth--
			if depth == 0 {
				return i
			}
			i += len(closeTarget)
		case strings.HasPrefix(lower[i:], openTarget) && hasXMLToolTagBoundary(s, i+len(openTarget)):
			depth++
			i += len(openTarget)
		default:
			i++
		}
	}
	return -1
}

func findXMLOpenOutsideCDATA(s, openTag string, start int) int {
	if s == "" || openTag == "" {
		return -1
	}
	if start < 0 {
		start = 0
	}
	lower := strings.ToLower(s)
	target := strings.ToLower(openTag)
	for i := start; i < len(s); {
		switch {
		case strings.HasPrefix(lower[i:], "<![cdata["):
			end := strings.Index(lower[i+len("<![cdata["):], "]]>")
			if end < 0 {
				return -1
			}
			i += len("<![cdata[") + end + len("]]>")
		case strings.HasPrefix(lower[i:], "<!--"):
			end := strings.Index(lower[i+len("<!--"):], "-->")
			if end < 0 {
				return -1
			}
			i += len("<!--") + end + len("-->")
		case strings.HasPrefix(lower[i:], target) && hasXMLToolTagBoundary(s, i+len(target)):
			return i
		default:
			i++
		}
	}
	return -1
}

func hasXMLToolTagBoundary(text string, idx int) bool {
	if idx >= len(text) {
		return true
	}
	switch text[idx] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	default:
		return false
	}
}

// hasOpenXMLToolTag returns true if captured text contains an XML tool opening tag
// whose SPECIFIC closing tag has not appeared yet.
func hasOpenXMLToolTag(captured string) bool {
	lower := strings.ToLower(captured)
	for _, pair := range xmlToolCallTagPairs {
		openIdx := strings.Index(lower, pair.open)
		if openIdx >= 0 {
			if findXMLCloseOutsideCDATA(captured, pair.close, openIdx+len(pair.open)) < 0 {
				return true
			}
		}
	}
	return false
}

func shouldKeepBareInvokeCapture(captured string) bool {
	lower := strings.ToLower(captured)
	invokeIdx, dsml := firstInvokeIndex(lower)
	if invokeIdx < 0 || containsAnyToolCallWrapper(lower) {
		return false
	}
	wrapperClose := "</tool_calls>"
	invokeOpenLen := len("<invoke")
	invokeClose := "</invoke>"
	parameterOpen := "<parameter"
	if dsml {
		wrapperClose = "</|dsml|tool_calls>"
		invokeOpenLen = len("<|dsml|invoke")
		invokeClose = "</|dsml|invoke>"
		parameterOpen = "<|dsml|parameter"
	}
	if findXMLCloseOutsideCDATA(captured, wrapperClose, invokeIdx) > invokeIdx {
		return true
	}

	startEnd := findXMLTagEnd(captured, invokeIdx+invokeOpenLen)
	if startEnd < 0 {
		return true
	}
	body := captured[startEnd+1:]
	trimmedBody := strings.TrimLeft(body, " \t\r\n")
	if trimmedBody == "" {
		return true
	}

	invokeCloseIdx := findXMLCloseOutsideCDATA(captured, invokeClose, startEnd+1)
	if invokeCloseIdx >= 0 {
		afterClose := captured[invokeCloseIdx+len(invokeClose):]
		return strings.TrimSpace(afterClose) == ""
	}

	trimmedLower := strings.ToLower(trimmedBody)
	return strings.HasPrefix(trimmedLower, parameterOpen) ||
		strings.HasPrefix(trimmedLower, "{") ||
		strings.HasPrefix(trimmedLower, "[")
}

func containsAnyToolCallWrapper(lower string) bool {
	return strings.Contains(lower, "<tool_calls") ||
		strings.Contains(lower, "<|dsml|tool_calls") ||
		strings.Contains(lower, "<dsml|tool_calls") ||
		strings.Contains(lower, "<｜tool_calls") ||
		strings.Contains(lower, "<|tool_calls")
}

func firstInvokeIndex(lower string) (int, bool) {
	xmlIdx := strings.Index(lower, "<invoke")
	// Check all DSML-like invoke prefixes.
	dsmlPrefixes := []string{"<|dsml|invoke", "<dsml|invoke", "<｜invoke", "<|invoke"}
	dsmlIdx := -1
	for _, prefix := range dsmlPrefixes {
		idx := strings.Index(lower, prefix)
		if idx >= 0 && (dsmlIdx < 0 || idx < dsmlIdx) {
			dsmlIdx = idx
		}
	}
	switch {
	case xmlIdx < 0:
		return dsmlIdx, dsmlIdx >= 0
	case dsmlIdx < 0:
		return xmlIdx, false
	case dsmlIdx < xmlIdx:
		return dsmlIdx, true
	default:
		return xmlIdx, false
	}
}

func findXMLCloseOutsideCDATA(s, closeTag string, start int) int {
	if s == "" || closeTag == "" {
		return -1
	}
	if start < 0 {
		start = 0
	}
	lower := strings.ToLower(s)
	target := strings.ToLower(closeTag)
	for i := start; i < len(s); {
		switch {
		case strings.HasPrefix(lower[i:], "<![cdata["):
			end := strings.Index(lower[i+len("<![cdata["):], "]]>")
			if end < 0 {
				return -1
			}
			i += len("<![cdata[") + end + len("]]>")
		case strings.HasPrefix(lower[i:], "<!--"):
			end := strings.Index(lower[i+len("<!--"):], "-->")
			if end < 0 {
				return -1
			}
			i += len("<!--") + end + len("-->")
		case strings.HasPrefix(lower[i:], target):
			return i
		default:
			i++
		}
	}
	return -1
}

func findXMLTagEnd(s string, start int) int {
	quote := byte(0)
	for i := start; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return i
		}
	}
	return -1
}

// findPartialXMLToolTagStart checks if the string ends with a partial canonical
// XML wrapper tag (e.g., "<too") and returns the position of the '<'.
func findPartialXMLToolTagStart(s string) int {
	lastLT := strings.LastIndex(s, "<")
	if lastLT < 0 {
		return -1
	}
	tail := s[lastLT:]
	// If there's a '>' in the tail, the tag is closed — not partial.
	if strings.Contains(tail, ">") {
		return -1
	}
	lowerTail := strings.ToLower(tail)
	// Check if the tail is a prefix of any known XML tool tag.
	for _, tag := range xmlToolCallOpeningTags {
		tagWithLT := tag
		if !strings.HasPrefix(tagWithLT, "<") {
			tagWithLT = "<" + tagWithLT
		}
		if strings.HasPrefix(tagWithLT, lowerTail) {
			return lastLT
		}
	}
	return -1
}
