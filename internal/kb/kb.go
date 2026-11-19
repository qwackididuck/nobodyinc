// Package kb is the project's knowledge base (ADR-0019, docs/knowledge.md):
// a folder of the project, declared in .nobody/org.yaml ("knowledge:
// {path: docs}"), whose markdown files are pages.
//
// The files are the only source of truth. This package reads them:
//
//   - kb.go: where the folder may be (CheckPath, Resolve) and the limits;
//   - page.go: one page, parsed with goldmark: its title, its YAML header,
//     its links and its plain text;
//   - base.go: the whole folder (Scan): the tree, each page's backlinks,
//     the broken links, the orphan pages and the home page;
//   - render.go: a page as HTML for the control room, raw HTML escaped and
//     links rewritten;
//   - service.go: the live knowledge base of a running daemon, scanned again
//     when it is read, and its full-text index (SQLite FTS5, internal/store),
//     derived from the files and rebuilt from them at will.
//
// The daemon reads the folder in the project's main checkout: during a
// feature, an agent's changes in its team's worktree are not seen until the
// feature is finished. Page contents are never logged: documentation may
// hold secrets.
package kb

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Limits. A page larger than MaxPageBytes is listed with its error, and
// neither parsed nor indexed; an asset larger than MaxAssetBytes is not
// served.
const (
	MaxPageBytes  = 1 << 20
	MaxAssetBytes = 10 << 20
)

// kilobyte converts sizes for error messages.
const kilobyte = 10 // bits to shift

// DefaultPath is the folder suggested when the human sets a knowledge base
// up.
const DefaultPath = "docs"

// FolderMode is the mode of a knowledge folder created by Nobody Inc: a
// project folder like any other.
const FolderMode = 0o755

// protectedDirs stay out of the knowledge base: agents never write the
// first two (internal/runtime/permissions.go), and none of them is
// documentation.
var protectedDirs = []string{".nobody", ".claude", ".git"}

// Errors about the knowledge base itself.
var (
	// ErrNone means the project has no knowledge base.
	ErrNone = errors.New("the project has no knowledge base (set one up in Settings › Project and daemon)")
	// ErrBadPath is a path the rules refuse (CheckPath, Resolve).
	ErrBadPath = errors.New("invalid knowledge base path")
	// ErrNotFound is a page or asset that is not in the knowledge base.
	ErrNotFound = errors.New("not found in the knowledge base")
)

// CheckPath validates the folder of a knowledge base as written in org.yaml
// and returns it cleaned, with forward slashes: a relative path, inside the
// project, that is not the project itself nor inside .nobody, .claude or
// .git. It only looks at the text; Resolve also follows symbolic links.
func CheckPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("%w: empty", ErrBadPath)
	}

	slashed := filepath.ToSlash(p)
	if path.IsAbs(slashed) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("%w %q: it must be relative to the project", ErrBadPath, p)
	}

	if slices.Contains(strings.Split(slashed, "/"), "..") {
		return "", fmt.Errorf("%w %q: it must stay inside the project (no \"..\")", ErrBadPath, p)
	}

	clean := path.Clean(slashed)
	if clean == "." {
		return "", fmt.Errorf("%w %q: choose a folder of the project, not the project itself", ErrBadPath, p)
	}

	if first, _, _ := strings.Cut(clean, "/"); slices.Contains(protectedDirs, first) {
		return "", fmt.Errorf("%w %q: %s is not documentation", ErrBadPath, p, first)
	}

	return clean, nil
}

// Resolve returns the absolute folder of a knowledge base declared as p in
// the project at projectDir, after CheckPath and with its symbolic links
// followed: it is refused when it leaves the project. A folder that does
// not exist yet is resolved through its deepest existing parent.
func Resolve(projectDir, p string) (string, error) {
	clean, err := CheckPath(p)
	if err != nil {
		return "", err
	}

	project, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve the project folder: %w", err)
	}

	resolved, err := evalExisting(filepath.Join(project, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}

	if !within(project, resolved) || resolved == project {
		return "", fmt.Errorf("%w %q: it leads outside the project, through a symbolic link", ErrBadPath, p)
	}

	rel, err := filepath.Rel(project, resolved)
	if err != nil {
		return "", err
	}

	if first, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); slices.Contains(protectedDirs, first) {
		return "", fmt.Errorf("%w %q: it leads into %s, through a symbolic link", ErrBadPath, p, first)
	}

	return resolved, nil
}

// evalExisting follows the symbolic links of the deepest existing part of
// p, and appends the rest as it is.
func evalExisting(p string) (string, error) {
	var rest []string

	for current := p; ; {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}

		rest = append([]string{filepath.Base(current)}, rest...)
		current = parent
	}
}

// within reports whether p is dir or inside it (both absolute and clean).
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CleanPagePath validates a page or asset path given by a client (the API,
// an agent's tool): relative to the knowledge folder, with forward slashes,
// no "..", no hidden segment. It returns it cleaned.
func CleanPagePath(p string) (string, error) {
	slashed := strings.TrimPrefix(filepath.ToSlash(p), "/")
	if slashed == "" {
		return "", fmt.Errorf("%w: empty path", ErrNotFound)
	}

	for seg := range strings.SplitSeq(slashed, "/") {
		if seg == ".." || strings.HasPrefix(seg, ".") && seg != "." {
			return "", fmt.Errorf("%w: %q", ErrNotFound, p)
		}
	}

	clean := path.Clean(slashed)
	if clean == "." {
		return "", fmt.Errorf("%w: %q", ErrNotFound, p)
	}

	return clean, nil
}
