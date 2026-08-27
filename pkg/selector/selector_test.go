package selector

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		expr          string
		expectedKind  Kind
		expectedName  string
		expectedValue string
		expectedError bool
	}{
		{
			name:         "topic_selector",
			expr:         "topic:business-critical-yes",
			expectedKind: KindTopic,
			expectedName: "business-critical-yes",
		},
		{
			name:          "property_selector",
			expr:          "property:business-critical=yes",
			expectedKind:  KindProperty,
			expectedName:  "business-critical",
			expectedValue: "yes",
		},
		{
			name:         "kind_is_case_insensitive",
			expr:         "TOPIC:production",
			expectedKind: KindTopic,
			expectedName: "production",
		},
		{
			name:          "surrounding_whitespace_is_trimmed",
			expr:          "  property: tier = tier-1 ",
			expectedKind:  KindProperty,
			expectedName:  "tier",
			expectedValue: "tier-1",
		},
		{
			name:          "property_value_may_contain_equals",
			expr:          "property:expr=a=b",
			expectedKind:  KindProperty,
			expectedName:  "expr",
			expectedValue: "a=b",
		},
		{
			name:          "missing_kind_separator",
			expr:          "business-critical-yes",
			expectedError: true,
		},
		{
			name:          "unsupported_kind",
			expr:          "label:business-critical-yes",
			expectedError: true,
		},
		{
			name:          "empty_topic_name",
			expr:          "topic:",
			expectedError: true,
		},
		{
			name:          "topic_with_value",
			expr:          "topic:business-critical=yes",
			expectedError: true,
		},
		{
			name:          "property_without_value",
			expr:          "property:business-critical",
			expectedError: true,
		},
		{
			name:          "property_with_empty_value",
			expr:          "property:business-critical=",
			expectedError: true,
		},
		{
			name:          "property_with_empty_name",
			expr:          "property:=yes",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Parse(tt.expr)

			if (err != nil) != tt.expectedError {
				t.Fatalf("Parse(%q) error = %v, expectedError %v", tt.expr, err, tt.expectedError)
			}
			if tt.expectedError {
				return
			}

			if s.Kind() != tt.expectedKind {
				t.Errorf("Parse(%q).Kind() = %q, want %q", tt.expr, s.Kind(), tt.expectedKind)
			}
			if s.Name() != tt.expectedName {
				t.Errorf("Parse(%q).Name() = %q, want %q", tt.expr, s.Name(), tt.expectedName)
			}
			if s.Value() != tt.expectedValue {
				t.Errorf("Parse(%q).Value() = %q, want %q", tt.expr, s.Value(), tt.expectedValue)
			}
		})
	}
}

func TestSelectorString(t *testing.T) {
	tests := []struct {
		name     string
		selector Selector
		expected string
	}{
		{
			name:     "topic",
			selector: NewTopic("business-critical-yes"),
			expected: "topic:business-critical-yes",
		},
		{
			name:     "property",
			selector: NewProperty("business-critical", "yes"),
			expected: "property:business-critical=yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.selector.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}

			// A rendered selector must parse back to an equal selector.
			reparsed, err := Parse(tt.expected)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.expected, err)
			}
			if reparsed != tt.selector {
				t.Errorf("Parse(String()) = %+v, want %+v", reparsed, tt.selector)
			}
		})
	}
}

func TestParseAll(t *testing.T) {
	tests := []struct {
		name          string
		exprs         []string
		expected      Set
		expectedError bool
	}{
		{
			name:     "empty_returns_defaults",
			exprs:    nil,
			expected: Defaults(),
		},
		{
			name:  "multiple_selectors",
			exprs: []string{"topic:production", "property:tier=1"},
			expected: Set{
				NewTopic("production"),
				NewProperty("tier", "1"),
			},
		},
		{
			name:          "one_invalid_selector_fails_all",
			exprs:         []string{"topic:production", "nonsense"},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := ParseAll(tt.exprs)

			if (err != nil) != tt.expectedError {
				t.Fatalf("ParseAll(%v) error = %v, expectedError %v", tt.exprs, err, tt.expectedError)
			}
			if tt.expectedError {
				return
			}

			if len(set) != len(tt.expected) {
				t.Fatalf("ParseAll(%v) = %v, want %v", tt.exprs, set, tt.expected)
			}
			for i := range set {
				if set[i] != tt.expected[i] {
					t.Errorf("ParseAll(%v)[%d] = %+v, want %+v", tt.exprs, i, set[i], tt.expected[i])
				}
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	set := Defaults()

	if !set.HasKind(KindTopic) || !set.HasKind(KindProperty) {
		t.Fatalf("Defaults() = %v, want both a topic and a property selector", set)
	}
	if !set.MatchesTopics([]string{"business-critical-yes"}) {
		t.Error("Defaults() does not match the legacy 'business-critical-yes' topic")
	}
	if !set.MatchesProperties(map[string][]string{"business-critical": {"yes"}}) {
		t.Error("Defaults() does not match the 'business-critical=yes' custom property")
	}
}

func TestSetHasKind(t *testing.T) {
	tests := []struct {
		name     string
		set      Set
		kind     Kind
		expected bool
	}{
		{"topic_present", Set{NewTopic("a")}, KindTopic, true},
		{"topic_absent", Set{NewProperty("a", "b")}, KindTopic, false},
		{"property_present", Set{NewTopic("a"), NewProperty("a", "b")}, KindProperty, true},
		{"property_absent", Set{NewTopic("a")}, KindProperty, false},
		{"empty_set", Set{}, KindTopic, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.set.HasKind(tt.kind); got != tt.expected {
				t.Errorf("HasKind(%q) = %v, want %v", tt.kind, got, tt.expected)
			}
		})
	}
}

func TestSetMatchesTopics(t *testing.T) {
	set := Set{NewTopic("business-critical-yes"), NewProperty("business-critical", "yes")}

	tests := []struct {
		name     string
		topics   []string
		expected bool
	}{
		{"exact_match", []string{"business-critical-yes"}, true},
		{"match_among_others", []string{"go", "business-critical-yes", "cli"}, true},
		{"case_insensitive", []string{"Business-Critical-Yes"}, true},
		{"no_match", []string{"other-topic"}, false},
		{"no_topics", nil, false},
		{"property_selector_does_not_match_topics", []string{"business-critical"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.MatchesTopics(tt.topics); got != tt.expected {
				t.Errorf("MatchesTopics(%v) = %v, want %v", tt.topics, got, tt.expected)
			}
		})
	}
}

func TestSetMatchesProperties(t *testing.T) {
	set := Set{NewTopic("business-critical-yes"), NewProperty("business-critical", "yes")}

	tests := []struct {
		name       string
		properties map[string][]string
		expected   bool
	}{
		{"exact_match", map[string][]string{"business-critical": {"yes"}}, true},
		{"case_insensitive_value", map[string][]string{"business-critical": {"Yes"}}, true},
		{"multi_select_contains_value", map[string][]string{"business-critical": {"no", "yes"}}, true},
		{"wrong_value", map[string][]string{"business-critical": {"no"}}, false},
		{"unset_value", map[string][]string{"business-critical": nil}, false},
		{"unknown_property", map[string][]string{"tier": {"yes"}}, false},
		{"no_properties", nil, false},
		{"topic_selector_does_not_match_properties", map[string][]string{"business-critical-yes": {"yes"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.MatchesProperties(tt.properties); got != tt.expected {
				t.Errorf("MatchesProperties(%v) = %v, want %v", tt.properties, got, tt.expected)
			}
		})
	}
}

func TestSetString(t *testing.T) {
	set := Set{NewTopic("production"), NewProperty("tier", "1")}

	expected := "topic:production, property:tier=1"
	if got := set.String(); got != expected {
		t.Errorf("String() = %q, want %q", got, expected)
	}
}

func TestParseErrorMentionsExpression(t *testing.T) {
	_, err := Parse("label:oops")
	if err == nil {
		t.Fatal("Parse(\"label:oops\") expected an error")
	}
	if !strings.Contains(err.Error(), "label:oops") {
		t.Errorf("error %q does not quote the offending expression", err)
	}
}
