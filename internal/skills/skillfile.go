package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
)

// File is the content of a SKILL.md (internal/frontmatter). Its "name" key
// is not kept: it is always the name of the skill's directory, written back
// by Format.
type File struct {
	frontmatter.File
}

// nameKey is the header key holding a skill's name.
const nameKey = "name"

// HasHooks reports whether the header declares hooks.
func (f File) HasHooks() bool {
	return f.Has("hooks")
}

// ParseFile reads a SKILL.md (frontmatter.Parse): a header that is not
// valid YAML is still read, as Claude Code still loads such a skill
// (checked by test/contract).
func ParseFile(data []byte) (File, error) {
	f, err := frontmatter.Parse(data, nameKey)

	return File{f}, err
}

// Format renders a SKILL.md for the skill name: name and description, the
// other keys, then the body.
func (f File) Format(name string) ([]byte, error) {
	return f.File.Format(frontmatter.Field{Key: nameKey, Value: name})
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
