package events_test

import (
	"testing"

	"github.com/lokeshgoel177/loki/internal/events"
)

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		topic    string
		expected bool
	}{
		// Exact matches
		{"exact match success", "session.created", "session.created", true},
		{"exact match different action", "session.created", "session.failed", false},
		{"exact match different prefix", "session.created", "user.created", false},

		// Global wildcard
		{"global wildcard matches simple topic", ">", "session.created", true},
		{"global wildcard matches nested topic", ">", "session.123.tool.output", true},
		{"global wildcard matches single segment", ">", "test", true},

		// Single segment wildcard (*)
		{"single wildcard matches middle token", "session.*.delta", "session.123.delta", true},
		{"single wildcard fails if extra token", "session.*.delta", "session.123.sub.delta", false},
		{"single wildcard fails if missing token", "session.*.delta", "session.delta", false},
		{"single wildcard matches first token", "*.created", "session.created", true},
		{"single wildcard matches first token different entity", "*.created", "tool.created", true},
		{"single wildcard fails when multiple prefix tokens", "*.created", "session.sub.created", false},
		{"single wildcard matches last token", "session.123.*", "session.123.delta", true},
		{"single wildcard at end fails on deeper nesting", "session.123.*", "session.123.tool.output", false},

		// Multi-segment wildcard (>)
		{"multi-segment wildcard matches single trailing", "session.123.>", "session.123.started", true},
		{"multi-segment wildcard matches multiple trailing", "session.123.>", "session.123.tool.output.chunk", true},
		{"multi-segment wildcard requires at least one token", "session.123.>", "session.123", false},
		{"multi-segment wildcard mismatch prefix", "session.123.>", "other.123.started", false},

		// Segment length mismatches
		{"pattern longer than topic", "session.123.tool.started", "session.123", false},
		{"topic longer than pattern", "session.123", "session.123.tool.started", false},
		{"both empty strings", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := events.MatchTopic(tc.pattern, tc.topic)
			if actual != tc.expected {
				t.Errorf("MatchTopic(%q, %q) = %v; want %v", tc.pattern, tc.topic, actual, tc.expected)
			}
		})
	}
}

func TestMatchesAnyTopic(t *testing.T) {
	patterns := []string{
		"session.created",
		"session.*.tool.>",
		"client.events_dropped",
	}

	tests := []struct {
		candidate string
		expected  bool
	}{
		{"session.created", true},
		{"session.123.tool.started", true},
		{"session.123.tool.output.chunk", true},
		{"client.events_dropped", true},
		{"session.123.message.delta", false},
		{"unknown.topic", false},
	}

	for _, tc := range tests {
		t.Run(tc.candidate, func(t *testing.T) {
			actual := events.MatchesAnyTopic(patterns, tc.candidate)
			if actual != tc.expected {
				t.Errorf("MatchesAnyTopic(%v, %q) = %v; want %v", patterns, tc.candidate, actual, tc.expected)
			}
		})
	}
}
