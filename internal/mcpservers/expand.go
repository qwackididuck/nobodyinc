package mcpservers

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// envReference matches ${NAME} and ${NAME:-default}, as Claude Code expands
// them in MCP configurations.
var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// ErrMissingVariable is returned when a server refers to an environment
// variable that is not set and has no default.
var ErrMissingVariable = errors.New("environment variable not set")

// References lists, sorted and without duplicates, the environment variables
// referred to anywhere in a value (a server's configuration, or part of it).
func References(v any) []string {
	var names []string

	walkStrings(v, func(s string) {
		for _, m := range envReference.FindAllStringSubmatch(s, -1) {
			if !slices.Contains(names, m[1]) {
				names = append(names, m[1])
			}
		}
	})

	slices.Sort(names)

	return names
}

// RefersToVariable reports whether s holds at least one ${NAME} reference:
// what every env and headers value written by Nobody Inc must do, so that no
// secret is ever written in clear.
func RefersToVariable(s string) bool {
	return envReference.MatchString(s)
}

// Expand replaces the ${NAME} and ${NAME:-default} references in s, with
// lookup giving the environment. A variable that is not set and has no
// default is an error naming it.
func Expand(s string, lookup func(string) (string, bool)) (string, error) {
	var missing []string

	out := envReference.ReplaceAllStringFunc(s, func(ref string) string {
		m := envReference.FindStringSubmatch(ref)
		hasDefault := strings.Contains(ref, ":-")

		// As in a shell, ${NAME:-default} also uses the default when NAME is
		// set but empty.
		if value, ok := lookup(m[1]); ok && (value != "" || !hasDefault) {
			return value
		}

		if hasDefault {
			return m[2]
		}

		missing = append(missing, m[1])

		return ref
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("%w: %s", ErrMissingVariable, strings.Join(missing, ", "))
	}

	return out, nil
}

// walkStrings calls fn on every string inside v (maps, lists, strings).
func walkStrings(v any, fn func(string)) {
	switch x := v.(type) {
	case string:
		fn(x)
	case map[string]any:
		for _, item := range x {
			walkStrings(item, fn)
		}
	case []any:
		for _, item := range x {
			walkStrings(item, fn)
		}
	}
}
