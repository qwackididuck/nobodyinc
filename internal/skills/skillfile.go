package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

// File is the content of a SKILL.md. The control room edits the description
// and the body; the other header keys (allowed-tools, hooks, model...) are
// kept as they are, in their order, comments included.
type File struct {
	Description string
	// Body is the markdown after the header: the skill's instructions.
	Body string
	// other holds the header's other keys and values, as YAML nodes (key,
	// value, key, value...). "name" is not kept: it is always the name of
	// the skill's directory.
	other []*yaml.Node
	// looseOther holds the header's other lines, as written, when the header
	// is not valid YAML (see parseLoose); other is then empty.
	looseOther string
}

// HasHooks reports whether the header declares hooks.
func (f File) HasHooks() bool {
	if f.looseOther != "" {
		return slices.ContainsFunc(strings.Split(f.looseOther, "\n"), func(line string) bool {
			return strings.HasPrefix(line, "hooks:")
		})
	}

	for i := 0; i+1 < len(f.other); i += 2 {
		if f.other[i].Value == "hooks" {
			return true
		}
	}

	return false
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

const fence = "---"

// ParseFile reads a SKILL.md. A file without a header is all body, as
// Claude Code reads it. A header that is not valid YAML, such as an
// unquoted description holding ": ", is read line by line, as Claude Code
// still loads such a skill (checked by test/contract).
func ParseFile(data []byte) (File, error) {
	header, body := splitHeader(strings.ReplaceAll(string(data), "\r\n", "\n"))

	f := File{Body: strings.TrimSpace(body)}
	if strings.TrimSpace(header) == "" {
		return f, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return parseLoose(header, f)
	}

	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return File{}, errors.New("header: expected keys and values")
	}

	pairs := doc.Content[0].Content
	for i := 0; i+1 < len(pairs); i += 2 {
		key, value := pairs[i], pairs[i+1]

		switch key.Value {
		case "name":
			// the directory's name wins (see File.other)
		case "description":
			f.Description = value.Value
		default:
			f.other = append(f.other, key, value)
		}
	}

	return f, nil
}

// parseLoose reads a header that is not valid YAML: each line starting
// without indentation is a "key: value", the indented lines after it belong
// to it. The description is the text after "description:" (quotes removed);
// the other keys are kept as written. Saving the skill writes a valid header,
// the description quoted when needed.
func parseLoose(header string, f File) (File, error) {
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
			if current == "description" {
				f.Description = value
			}
		}

		if current != "name" && current != "description" {
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

// Format renders a SKILL.md for the skill name: name and description, the
// other keys, then the body.
func (f File) Format(name string) ([]byte, error) {
	pairs := []*yaml.Node{
		scalar("name"), scalar(name),
		scalar("description"), scalar(f.Description),
	}

	header, err := marshalMapping(append(pairs, f.other...))
	if err != nil {
		return nil, err
	}

	header = append(header, f.looseOther...)

	var b bytes.Buffer

	b.WriteString(fence + "\n")
	b.Write(header)
	b.WriteString(fence + "\n")

	if body := strings.TrimSpace(f.Body); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}

	return b.Bytes(), nil
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func marshalMapping(pairs []*yaml.Node) ([]byte, error) {
	var b bytes.Buffer

	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)

	if err := enc.Encode(&yaml.Node{Kind: yaml.MappingNode, Content: pairs}); err != nil {
		return nil, fmt.Errorf("encode skill header: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode skill header: %w", err)
	}

	return b.Bytes(), nil
}

// ReadFile reads the SKILL.md of the skill in dir.
func ReadFile(dir string) (File, error) {
	data, err := os.ReadFile(filepath.Join(dir, skillFile))
	if err != nil {
		return File{}, err
	}

	return ParseFile(data)
}

// Save writes the SKILL.md of the skill in dir, creating the directory:
// description and body from the arguments, the other header keys of an
// existing file kept. The skill's other files are not touched.
func Save(dir, description, body string) error {
	if strings.TrimSpace(description) == "" {
		return errors.New("a skill needs a description: the model chooses skills from it")
	}

	f, err := ReadFile(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read the current skill: %w", err)
	}

	f.Description = strings.TrimSpace(description)
	f.Body = body

	data, err := f.Format(filepath.Base(dir))
	if err != nil {
		return err
	}

	return atomicfile.Write(filepath.Join(dir, skillFile), data)
}

// Delete removes the skill's whole directory. A personal skill reached
// through a symbolic link loses the link only.
func Delete(dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, skillFile)); err != nil {
		if _, linkErr := os.Lstat(dir); linkErr != nil {
			return err
		}
	}

	return os.RemoveAll(dir)
}

// Copy copies the skill directory from into to, which must not exist: the
// SKILL.md and every other file, symbolic links inside followed.
func Copy(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s: %w", to, fs.ErrExist)
	}

	src := os.DirFS(from)

	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		target := filepath.Join(to, filepath.FromSlash(path))

		if d.IsDir() {
			return os.MkdirAll(target, dirMode)
		}

		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}

		info, err := fs.Stat(src, path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
