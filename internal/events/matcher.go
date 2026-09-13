package events

import "strings"

// MatchTopic checks whether a candidate topic matches a subscription pattern.
//
// Supported patterns:
//   - ">" matches any topic (global wildcard).
//   - "*" matches exactly one dot-separated segment (e.g. "session.*.delta").
//   - ">" at the end matches one or more trailing segments (e.g. "session.abc.>").
//   - Exact string match (e.g. "session.created").
func MatchTopic(pattern, topic string) bool {
	// Fast path: global wildcard or exact match
	if pattern == ">" || pattern == topic {
		return true
	}

	pTokens := strings.Split(pattern, ".")
	tTokens := strings.Split(topic, ".")

	for i := 0; i < len(pTokens); i++ {
		pTok := pTokens[i]

		// Multi-segment wildcard must be the last token in pattern
		if pTok == ">" {
			return i < len(tTokens)
		}

		// Pattern is longer than candidate topic
		if i >= len(tTokens) {
			return false
		}

		// Single-segment wildcard matches any single token
		if pTok == "*" {
			continue
		}

		// Literal match check
		if pTok != tTokens[i] {
			return false
		}
	}

	// Pattern matched all checked tokens; valid only if topic has no extra tokens
	return len(pTokens) == len(tTokens)
}

// MatchesAnyTopic checks if candidate topic matches any of the given patterns.
func MatchesAnyTopic(patterns []string, candidate string) bool {
	for _, p := range patterns {
		if MatchTopic(p, candidate) {
			return true
		}
	}
	return false
}
