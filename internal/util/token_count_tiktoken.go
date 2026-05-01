//go:build !386 && !arm && !mips && !mipsle && !wasm

package util

import (
	"strings"

	tiktoken "github.com/hupe1980/go-tiktoken"
)

func countWithTokenizer(text, model string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	encoding, err := tiktoken.NewEncodingForModel(tokenizerModelForCount(model))
	if err != nil {
		return 0
	}
	ids, _, err := encoding.Encode(text, nil, nil)
	if err != nil {
		return 0
	}
	return len(ids)
}

func tokenizerModelForCount(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return defaultTokenizerModel
	}
	switch {
	case strings.HasPrefix(model, "claude"):
		return claudeTokenizerModel
	case strings.HasPrefix(model, "gpt-4"), strings.HasPrefix(model, "gpt-5"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return defaultTokenizerModel
	case strings.HasPrefix(model, "deepseek-v4"):
		return defaultTokenizerModel
	case strings.HasPrefix(model, "deepseek"):
		return defaultTokenizerModel
	default:
		return defaultTokenizerModel
	}
}
