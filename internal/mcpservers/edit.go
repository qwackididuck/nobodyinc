package mcpservers

import (
	"maps"
	"slices"
)

// Editing a server (ADR-0010). The control room gets the server with its
// command, arguments and URL, which it hides until the human reveals them,
// and its env and headers values that refer to variables; a value written in
// clear is never sent. A change replaces the transport and the variables, and
// keeps the server's other keys.

// editableKeys are the keys a change replaces; the others are kept.
var editableKeys = []string{"type", "command", "args", "url", "env", "headers"}

// Redact returns a copy of cfg whose env and headers values written in clear
// are emptied, and the list of those values, as "env.NAME" or "headers.NAME".
func Redact(cfg Config) (Config, []string) {
	out := maps.Clone(cfg)
	cleared := []string{}

	for _, field := range []string{"env", "headers"} {
		values, ok := cfg[field].(map[string]any)
		if !ok {
			continue
		}

		redacted := map[string]any{}

		for _, key := range slices.Sorted(maps.Keys(values)) {
			if s, isString := values[key].(string); isString && RefersToVariable(s) {
				redacted[key] = s

				continue
			}

			redacted[key] = ""
			cleared = append(cleared, field+"."+key)
		}

		out[field] = redacted
	}

	return out, cleared
}

// Replace returns current with its transport and variables replaced by those
// of change, its other keys (e.g. a timeout) kept.
func Replace(current, change Config) Config {
	out := maps.Clone(current)

	for _, key := range editableKeys {
		delete(out, key)

		if value, ok := change[key]; ok {
			out[key] = value
		}
	}

	return out
}
