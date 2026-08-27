// Package selector defines the criteria used to decide whether a repository
// is a "production" repository.
package selector

import (
	"fmt"
	"strings"
)

// Kind identifies which GitHub metadata a Selector matches against.
type Kind string

const (
	// KindTopic matches against a repository's topics.
	KindTopic Kind = "topic"
	// KindProperty matches against an organization custom property value.
	KindProperty Kind = "property"
)

// Default selectors, applied when the user supplies none. They cover both the
// legacy topic convention and its custom property successor.
const (
	defaultTopic         = "topic:business-critical-yes"
	defaultPropertyValue = "property:business-critical=yes"
)

// Selector is a single criterion that marks a repository as production.
// A topic selector carries only a name; a property selector carries a name
// and the value that name must hold.
type Selector struct {
	kind  Kind
	name  string
	value string
}

// NewTopic returns a Selector matching repositories carrying the given topic.
func NewTopic(name string) Selector {
	return Selector{kind: KindTopic, name: name}
}

// NewProperty returns a Selector matching repositories whose custom property
// name holds value.
func NewProperty(name, value string) Selector {
	return Selector{kind: KindProperty, name: name, value: value}
}

// Kind reports which metadata this Selector matches against.
func (s Selector) Kind() Kind { return s.kind }

// Name reports the topic or custom property name.
func (s Selector) Name() string { return s.name }

// Value reports the required custom property value, empty for topics.
func (s Selector) Value() string { return s.value }

// String renders the Selector in the syntax accepted by Parse.
func (s Selector) String() string {
	if s.kind == KindProperty {
		return fmt.Sprintf("%s:%s=%s", s.kind, s.name, s.value)
	}
	return fmt.Sprintf("%s:%s", s.kind, s.name)
}

// Parse reads a selector expression of the form 'topic:NAME' or
// 'property:NAME=VALUE'.
func Parse(expr string) (Selector, error) {
	kind, rest, found := strings.Cut(strings.TrimSpace(expr), ":")
	if !found {
		return Selector{}, fmt.Errorf("invalid selector %q: expected 'topic:NAME' or 'property:NAME=VALUE'", expr)
	}

	switch Kind(strings.ToLower(strings.TrimSpace(kind))) {
	case KindTopic:
		name := strings.TrimSpace(rest)
		if name == "" {
			return Selector{}, fmt.Errorf("invalid selector %q: topic name is empty", expr)
		}
		if strings.Contains(name, "=") {
			return Selector{}, fmt.Errorf("invalid selector %q: topics do not take a value", expr)
		}
		return NewTopic(name), nil
	case KindProperty:
		name, value, hasValue := strings.Cut(rest, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if !hasValue {
			return Selector{}, fmt.Errorf("invalid selector %q: property requires a value, e.g. 'property:%s=yes'", expr, name)
		}
		if name == "" {
			return Selector{}, fmt.Errorf("invalid selector %q: property name is empty", expr)
		}
		if value == "" {
			return Selector{}, fmt.Errorf("invalid selector %q: property value is empty", expr)
		}
		return NewProperty(name, value), nil
	default:
		return Selector{}, fmt.Errorf("unsupported selector kind %q in %q (must be topic or property)", kind, expr)
	}
}

// Set is a group of selectors combined with OR semantics: a repository is
// production if it satisfies any one of them.
type Set []Selector

// Defaults returns the selectors used when the user supplies none.
func Defaults() Set {
	set, err := ParseAll([]string{defaultTopic, defaultPropertyValue})
	if err != nil {
		panic(fmt.Sprintf("invalid default selectors: %v", err))
	}
	return set
}

// ParseAll parses each expression, returning Defaults when exprs is empty.
func ParseAll(exprs []string) (Set, error) {
	if len(exprs) == 0 {
		return Defaults(), nil
	}

	set := make(Set, 0, len(exprs))
	for _, expr := range exprs {
		s, err := Parse(expr)
		if err != nil {
			return nil, err
		}
		set = append(set, s)
	}
	return set, nil
}

// HasKind reports whether any Selector in the Set matches against kind. It
// lets callers skip API calls no Selector needs.
func (set Set) HasKind(kind Kind) bool {
	for _, s := range set {
		if s.kind == kind {
			return true
		}
	}
	return false
}

// String renders the Set as a comma-separated list of selector expressions.
func (set Set) String() string {
	exprs := make([]string, len(set))
	for i, s := range set {
		exprs[i] = s.String()
	}
	return strings.Join(exprs, ", ")
}

// MatchesTopics reports whether any topic Selector in the Set is satisfied by
// the given topics. GitHub normalizes topics to lower case.
func (set Set) MatchesTopics(topics []string) bool {
	for _, s := range set {
		if s.kind != KindTopic {
			continue
		}
		for _, topic := range topics {
			if strings.EqualFold(topic, s.name) {
				return true
			}
		}
	}
	return false
}

// MatchesProperties reports whether any property Selector in the Set is
// satisfied by the given custom property values, keyed by property name.
// A multi-select property matches when any of its values matches.
func (set Set) MatchesProperties(properties map[string][]string) bool {
	for _, s := range set {
		if s.kind != KindProperty {
			continue
		}
		for _, value := range properties[strings.ToLower(s.name)] {
			if strings.EqualFold(value, s.value) {
				return true
			}
		}
	}
	return false
}
