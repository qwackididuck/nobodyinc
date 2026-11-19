package commands

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

// The header keys Nobody Inc reads, besides the description.
const (
	argumentHintKey = "argument-hint"
	allowedToolsKey = "allowed-tools"
)

// File is the content of a command file (internal/frontmatter): the control
// room edits its description and prompt, the other header keys are kept as
// they are.
type File struct {
	frontmatter.File
}

// ParseFile reads a command file. A file without a header is all prompt, as
// Claude Code reads it.
func ParseFile(data []byte) (File, error) {
	f, err := frontmatter.Parse(data)

	return File{f}, err
}

// ReadFile reads the command file at path.
func ReadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}

	return ParseFile(data)
}

// Save writes the command file at path, creating its directories:
// description and prompt from the arguments, the other header keys of an
// existing file kept. The description may be empty (Claude Code then shows
// the prompt's first line); the prompt may not.
//
// A command file that is a symbolic link (personal commands kept in a
// dotfiles repository, say) is written through: the link stays, its target
// changes. Written atomically, the link itself would be replaced by a file.
func Save(path, description, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a command needs a prompt: it is what the command sends")
	}

	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}

	f, err := ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read the current command: %w", err)
	}

	f.Description = strings.TrimSpace(description)
	f.Body = body

	data, err := f.Format()
	if err != nil {
		return err
	}

	return atomicfile.Write(path, data)
}

// Delete removes the command file at path, then the directories under root
// it leaves empty. A personal command reached through a symbolic link loses
// the link only.
func Delete(root, path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	removeEmptyParents(root, filepath.Dir(path))

	return nil
}

// removeEmptyParents removes dir and its parents up to root, excluded, as
// long as they are empty. Errors are ignored: an empty directory left behind
// holds no command.
func removeEmptyParents(root, dir string) {
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if os.Remove(dir) != nil { // fails on a non-empty directory
			return
		}

		dir = filepath.Dir(dir)
	}
}

// Copy copies the command file from into to, which must not exist, creating
// its directories.
func Copy(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s: %w", to, fs.ErrExist)
	}

	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}

	return atomicfile.Write(to, data)
}
