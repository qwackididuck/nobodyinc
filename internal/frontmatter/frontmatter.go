// Package frontmatter reads and writes the markdown files Claude Code reads
// skills and commands from: an optional YAML header between "---" lines,
// then a markdown body.
//
// The control room edits the description and the body; the other header keys
// (allowed-tools, hooks, model...) are kept as they are, in their order,
// comments included. A header that is not valid YAML, such as an unquoted
// description holding ": ", is read line by line, as Claude Code still loads
// such a file (checked by test/contract); writing it back gives a valid
// header.
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// File is the content of a skill or command file.
type File struct {
	Description string
	// Body is the markdown after the header: the instructions.
	Body string
	// other holds the header's other keys and values, as YAML nodes (key,
	// value, key, value...).
	other []*yaml.Node
	// looseOther holds the header's other lines, as written, when the header
	// is not valid YAML (see parseLoose); other is then empty.
	looseOther string
}

// descriptionKey is the one header key File edits.
const descriptionKey = "description"

const fence = "---"

// Parse reads a file. A file without a header is all body, as Claude Code
// reads it. The header keys listed in dropped are discarded: the caller
// writes them itself (a skill's name is always its directory's).
func Parse(data []byte, dropped ...string) (File, error) {
	header, body := splitHeader(strings.ReplaceAll(string(data), "\r\n", "\n"))

	f := File{Body: strings.TrimSpace(body)}
	if strings.TrimSpace(header) == "" {
		return f, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return parseLoose(header, f, dropped)
	}

	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return File{}, errors.New("header: expected keys and values")
	}

	pairs := doc.Content[0].Content
	for i := 0; i+1 < len(pairs); i += 2 {
		key, value := pairs[i], pairs[i+1]

		switch {
		case slices.Contains(dropped, key.Value):
		case key.Value == descriptionKey:
			f.Description = value.Value
		default:
			f.other = append(f.other, key, value)
		}
	}

	return f, nil
}

// Has reports whether the header declares key (other than the description).
func (f File) Has(key string) bool {
	if f.looseOther != "" {
		return slices.ContainsFunc(strings.Split(f.looseOther, "\n"), func(line string) bool {
			return strings.HasPrefix(line, key+":")
		})
	}

	for i := 0; i+1 < len(f.other); i += 2 {
		if f.other[i].Value == key {
			return true
		}
	}

	return false
}

// Keys returns the header's other keys (the description left out), in
// order.
func (f File) Keys() []string {
	var keys []string

	if f.looseOther != "" {
		for line := range strings.SplitSeq(f.looseOther, "\n") {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
				continue
			}

			if key, _, ok := strings.Cut(line, ":"); ok {
				keys = append(keys, strings.TrimSpace(key))
			}
		}

		return keys
	}

	for i := 0; i+1 < len(f.other); i += 2 {
		keys = append(keys, f.other[i].Value)
	}

	return keys
}

// Value returns the value of a header key (other than the description), ""
// when it is absent: a scalar as it is, a list as its items joined with
// ", " (in brackets when written in brackets: "argument-hint: [pr]" is a
// list for YAML, but reads "[pr]"), anything else as YAML. In a header that
// is not valid YAML, it is the text after "key:" on its line.
func (f File) Value(key string) string {
	if f.looseOther != "" {
		for line := range strings.SplitSeq(f.looseOther, "\n") {
			if _, value, ok := strings.Cut(line, key+":"); ok && strings.HasPrefix(line, key+":") {
				return strings.Trim(strings.TrimSpace(value), `"'`)
			}
		}

		return ""
	}

	for i := 0; i+1 < len(f.other); i += 2 {
		if f.other[i].Value == key {
			return nodeText(f.other[i+1])
		}
	}

	return ""
}

// nodeText renders a header value for Value.
func nodeText(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.SequenceNode:
		items := make([]string, 0, len(n.Content))
		for _, item := range n.Content {
			items = append(items, nodeText(item))
		}

		if n.Style&yaml.FlowStyle != 0 {
			return "[" + strings.Join(items, ", ") + "]"
		}

		return strings.Join(items, ", ")
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		data, err := yaml.Marshal(n)
		if err != nil {
			return ""
		}

		return strings.TrimSpace(string(data))
	default:
		return ""
	}
}

// OtherKeys renders the header keys the control room does not edit, as YAML;
// "" when there are none.
func (f File) OtherKeys() (string, error) {
	if f.looseOther != "" {
		return f.looseOther, nil
	}

	if len(f.other) == 0 {
		return "", nil
	}

	data, err := marshalMapping(f.other)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Field is a header key and its value, for Format.
type Field struct {
	Key, Value string
}

// Format renders the file: the leading fields, the description when there
// is one, the other keys, then the body. A file with no header key at all
// is written as its body alone.
func (f File) Format(leading ...Field) ([]byte, error) {
	var pairs []*yaml.Node
	for _, field := range leading {
		pairs = append(pairs, scalar(field.Key), scalar(field.Value))
	}

	if f.Description != "" {
		pairs = append(pairs, scalar(descriptionKey), scalar(f.Description))
	}

	pairs = append(pairs, f.other...)

	var b bytes.Buffer

	if len(pairs) > 0 || f.looseOther != "" {
		header, err := marshalMapping(pairs)
		if err != nil {
			return nil, err
		}

		if len(pairs) == 0 {
			header = nil // only loose lines: "{}" is not a header
		}

		b.WriteString(fence + "\n")
		b.Write(header)
		b.WriteString(f.looseOther)
		b.WriteString(fence + "\n")
	}

	if body := strings.TrimSpace(f.Body); body != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}

		b.WriteString(body)
		b.WriteString("\n")
	}

	return b.Bytes(), nil
}

// parseLoose reads a header that is not valid YAML: each line starting
// without indentation is a "key: value", the indented lines after it belong
// to it. The description is the text after "description:" (quotes removed);
// the other keys are kept as written, but those listed in dropped.
func parseLoose(header string, f File, dropped []string) (File, error) {
	var (
		other   []string
		current string // the key the lines being read belong to
	)

	for line := range strings.SplitSeq(header, "\n") {
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			key, value, err := looseKey(line)
			if err != nil {
				return File{}, err
			}

			current = key
			if current == descriptionKey {
				f.Description = value
			}
		}

		if current != descriptionKey && !slices.Contains(dropped, current) {
			other = append(other, line)
		}
	}

	f.looseOther = strings.TrimSpace(strings.Join(other, "\n"))
	if f.looseOther != "" {
		f.looseOther += "\n"
	}

	return f, nil
}

// looseKey reads a header line without indentation: "key: value", with the
// value's quotes removed. A blank line or a comment has no key.
func looseKey(line string) (string, string, error) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			return "", "", fmt.Errorf("header: %q is neither a key nor a comment", line)
		}

		return "", "", nil
	}

	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`), nil
}

// splitHeader separates the YAML header between "---" lines from the body;
// without an opening fence, there is no header.
func splitHeader(s string) (string, string) {
	rest, ok := strings.CutPrefix(s, fence+"\n")
	if !ok {
		return "", s
	}

	if body, ok := strings.CutPrefix(rest, fence+"\n"); ok {
		return "", body
	}

	if header, body, ok := strings.Cut(rest, "\n"+fence+"\n"); ok {
		return header, body
	}

	if header, ok := strings.CutSuffix(rest, "\n"+fence); ok {
		return header, ""
	}

	return "", s // never closed: Claude Code would read it all as body
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func marshalMapping(pairs []*yaml.Node) ([]byte, error) {
	var b bytes.Buffer

	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)

	if err := enc.Encode(&yaml.Node{Kind: yaml.MappingNode, Content: pairs}); err != nil {
		return nil, fmt.Errorf("encode header: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode header: %w", err)
	}

	return b.Bytes(), nil
}
