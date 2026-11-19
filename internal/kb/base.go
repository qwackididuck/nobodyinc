package kb

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Base is the knowledge folder as read at one moment: a snapshot, never
// changed once returned by Scan.
type Base struct {
	// Path is the folder as declared in org.yaml ("docs"); Root is where it
	// is on disk, symbolic links followed.
	Path string
	Root string
	// Missing is true when the folder does not exist (deleted by hand, or
	// absent on the main checkout's branch): the base is then empty.
	Missing bool
	// Pages are every page, sorted by path; files every regular file of the
	// folder (pages included), for the links.
	Pages []*Page
	files map[string]bool
	index map[string]*Page
	// backlinks maps a page's path to the pages linking to it, sorted.
	backlinks map[string][]string
}

// Scan reads the knowledge folder at root, declared as declared. Hidden
// files and folders (".something") are skipped, and so are symbolic links
// to folders; a symbolic link to a file is read when its target stays in
// the folder. A page of previous whose size and modification time did not
// change is reused as it is, without reading the file again.
func Scan(declared, root string, previous *Base) (*Base, error) {
	// Files are checked against the root with their links followed: the
	// root must be too (on macOS, /var is a link to /private/var).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	b := &Base{Path: declared, Root: root, files: map[string]bool{}, index: map[string]*Page{}}

	info, err := os.Stat(root)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		b.Missing = true
	case err != nil:
		return nil, fmt.Errorf("read the knowledge folder %s: %w", declared, err)
	case !info.IsDir():
		return nil, fmt.Errorf("the knowledge base %s is not a folder", declared)
	default:
		if err := b.walk(previous); err != nil {
			return nil, fmt.Errorf("read the knowledge folder %s: %w", declared, err)
		}
	}

	slices.SortFunc(b.Pages, func(x, y *Page) int { return cmp.Compare(x.Path, y.Path) })
	b.link()

	return b, nil
}

// Page finds a page by path.
func (b *Base) Page(p string) (*Page, error) {
	clean, err := CleanPagePath(p)
	if err != nil {
		return nil, err
	}

	page := b.page(clean)
	if page == nil {
		return nil, fmt.Errorf("%w: page %s", ErrNotFound, clean)
	}

	return page, nil
}

// Backlinks lists the pages linking to the page at p, sorted.
func (b *Base) Backlinks(p string) []string {
	return slices.Clone(b.backlinks[p])
}

// HasFile reports whether p is a file of the knowledge folder.
func (b *Base) HasFile(p string) bool {
	return b.files[p]
}

// Home is the home page: index.md, else README.md, at the root of the
// folder; "" when neither exists (the control room then shows a generated
// overview).
func (b *Base) Home() string {
	for _, name := range []string{"index.md", "README.md"} {
		if p := b.page(name); p != nil && p.Err == "" {
			return name
		}
	}

	return ""
}

// BrokenLink is a link of a page that leads to nothing.
type BrokenLink struct {
	Page   string
	Target string
	Text   string
}

// Broken lists every broken page or asset link, page by page.
func (b *Base) Broken() []BrokenLink {
	var broken []BrokenLink

	for _, p := range b.Pages {
		for _, l := range p.Links {
			if l.Broken {
				broken = append(broken, BrokenLink{Page: p.Path, Target: l.Target, Text: l.Text})
			}
		}
	}

	return broken
}

// Orphans lists the pages no other page links to, the home page aside.
func (b *Base) Orphans() []string {
	home := b.Home()

	var orphans []string

	for _, p := range b.Pages {
		if p.Path != home && p.Err == "" && len(b.backlinks[p.Path]) == 0 {
			orphans = append(orphans, p.Path)
		}
	}

	return orphans
}

// Errors lists the pages that could not be read or parsed.
func (b *Base) Errors() []*Page {
	var errs []*Page

	for _, p := range b.Pages {
		if p.Err != "" {
			errs = append(errs, p)
		}
	}

	return errs
}

// Recent lists the n pages changed last (modification time), newest first.
func (b *Base) Recent(n int) []*Page {
	pages := slices.Clone(b.Pages)
	slices.SortStableFunc(pages, func(x, y *Page) int { return y.Modified.Compare(x.Modified) })

	return pages[:min(n, len(pages))]
}

// Folder is a top-level folder of the knowledge base, with its number of
// pages (sub-folders included).
type Folder struct {
	Path  string
	Pages int
}

// Folders lists the top-level folders holding pages, sorted.
func (b *Base) Folders() []Folder {
	counts := map[string]int{}

	for _, p := range b.Pages {
		if first, _, nested := strings.Cut(p.Path, "/"); nested {
			counts[first]++
		}
	}

	folders := make([]Folder, 0, len(counts))
	for name, n := range counts {
		folders = append(folders, Folder{Path: name, Pages: n})
	}

	slices.SortFunc(folders, func(x, y Folder) int { return cmp.Compare(x.Path, y.Path) })

	return folders
}

// Node is one entry of the tree: a folder with its children, or a page.
type Node struct {
	Name string
	Path string
	Dir  bool
	// Title and Err are a page's.
	Title string
	Err   string
	// Pages counts a folder's pages, sub-folders included.
	Pages    int
	Children []*Node
}

// Tree is the folder as a tree: folders holding pages first, then pages,
// each sorted by name. Folders without any page are left out.
func (b *Base) Tree() *Node {
	root := &Node{Name: b.Path, Path: "", Dir: true}

	for _, p := range b.Pages {
		parent := root
		parent.Pages++

		segments := strings.Split(p.Path, "/")
		for i, seg := range segments[:len(segments)-1] {
			parent = parent.child(seg, strings.Join(segments[:i+1], "/"))
			parent.Pages++
		}

		parent.Children = append(parent.Children, &Node{Name: segments[len(segments)-1], Path: p.Path, Title: p.Title, Err: p.Err})
	}

	root.sort()

	return root
}

// List lists what a folder of the knowledge base holds, for an agent's
// kb_list: its sub-folders (with their number of pages) and its pages. ""
// is the root.
func (b *Base) List(folder string) ([]*Node, []*Node, error) {
	var dirs, pages []*Node

	node := b.Tree()

	if folder = strings.Trim(path.Clean("/"+folder), "/"); folder != "" {
		for seg := range strings.SplitSeq(folder, "/") {
			node = node.find(seg)
			if node == nil {
				return nil, nil, fmt.Errorf("%w: folder %s", ErrNotFound, folder)
			}
		}
	}

	for _, c := range node.Children {
		if c.Dir {
			dirs = append(dirs, c)
		} else {
			pages = append(pages, c)
		}
	}

	return dirs, pages, nil
}

// Source reads a page's markdown as it is on disk, for an agent's kb_read
// (Page.Err pages are refused).
func (b *Base) Source(p string) ([]byte, *Page, error) {
	page, err := b.Page(p)
	if err != nil {
		return nil, nil, err
	}

	if page.Err != "" {
		return nil, nil, fmt.Errorf("%s: %s", page.Path, page.Err)
	}

	data, err := b.read(page.Path, MaxPageBytes)
	if err != nil {
		return nil, nil, err
	}

	return trimBOM(data), page, nil
}

// Asset reads a file of the knowledge folder that a page may show, after
// checking its path again on disk (no symbolic link out of the folder).
func (b *Base) Asset(p string) ([]byte, error) {
	clean, err := CleanPagePath(p)
	if err != nil {
		return nil, err
	}

	if !b.files[clean] {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, clean)
	}

	return b.read(clean, MaxAssetBytes)
}

// walk records every entry of the folder, hidden ones aside.
func (b *Base) walk(previous *Base) error {
	return filepath.WalkDir(b.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if p == b.Root {
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(b.Root, p)
		if err != nil {
			return err
		}

		b.addFile(filepath.ToSlash(rel), p, d, previous)

		return nil
	})
}

// addFile records one file of the folder, and a page when it is markdown.
func (b *Base) addFile(rel, full string, d fs.DirEntry, previous *Base) {
	readable, escapes := b.fileState(full, d)
	if !readable {
		return
	}

	if !escapes {
		b.files[rel] = true
	}

	if isPage(rel) {
		b.addPage(rel, full, escapes, previous)
	}
}

// fileState tells whether a folder entry is a file to record (a regular
// file, or a symbolic link to one), and then whether it is a symbolic link
// leading out of the folder.
func (b *Base) fileState(full string, d fs.DirEntry) (bool, bool) {
	if d.Type()&fs.ModeSymlink == 0 {
		return d.Type().IsRegular(), false
	}

	target, err := filepath.EvalSymlinks(full)
	if err != nil {
		return false, false
	}

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return false, false // a link to a folder is skipped
	}

	return true, !within(b.Root, target)
}

// addPage records a page: reused from previous when its file did not
// change, read and parsed otherwise.
func (b *Base) addPage(rel, full string, escapes bool, previous *Base) {
	page := &Page{Path: rel, Title: strings.TrimSuffix(path.Base(rel), path.Ext(rel))}
	b.Pages = append(b.Pages, page)
	b.index[rel] = page

	if escapes {
		page.Err = "a symbolic link leading out of the knowledge folder: not read"

		return
	}

	info, err := os.Stat(full)
	if err != nil {
		page.Err = "cannot be read: " + err.Error()

		return
	}

	page.Size, page.Modified = info.Size(), info.ModTime()

	if old := previous.page(rel); old != nil && old.Err == "" && old.Size == page.Size && old.Modified.Equal(page.Modified) {
		*page = *old
		// The previous snapshot may still be read: its links are resolved
		// again here (link), on a copy.
		page.Links = slices.Clone(old.Links)

		return
	}

	readPage(page, full)
}

// readPage reads and parses a page's file, unless it is too large.
func readPage(page *Page, full string) {
	if page.Size > MaxPageBytes {
		page.Err = fmt.Sprintf("%d KB, over the %d KB limit: listed, not indexed", page.Size>>kilobyte, MaxPageBytes>>kilobyte)

		return
	}

	data, err := os.ReadFile(full)
	if err != nil {
		page.Err = "cannot be read: " + err.Error()

		return
	}

	data = trimBOM(data)
	sum := sha256.Sum256(data)
	p := parsePage(page.Path, data)

	page.Digest = hex.EncodeToString(sum[:])
	page.Title, page.Header, page.Links, page.Text = p.title, p.header, p.links, p.text
}

// link resolves every page's links against the files, then builds the
// backlinks.
func (b *Base) link() {
	b.backlinks = map[string][]string{}

	// A page reused from a previous scan has its links resolved against the
	// files of then: they are resolved again from what the page says.
	for _, p := range b.Pages {
		for i, l := range p.Links {
			p.Links[i] = resolveLink(p.Path, Link{Target: l.Target, Text: l.Text, Image: l.Image}, b.files)
		}
	}

	for _, p := range b.Pages {
		for _, l := range p.Links {
			if l.Kind != LinkPage || l.Broken || l.Path == p.Path {
				continue
			}

			if !slices.Contains(b.backlinks[l.Path], p.Path) {
				b.backlinks[l.Path] = append(b.backlinks[l.Path], p.Path)
			}
		}
	}

	for _, from := range b.backlinks {
		slices.Sort(from)
	}
}

// page finds a page by path; nil when absent (b may be nil).
func (b *Base) page(p string) *Page {
	if b == nil {
		return nil
	}

	return b.index[p]
}

// read reads a file of the folder, refusing one that leaves it or is larger
// than limit.
func (b *Base) read(rel string, limit int64) ([]byte, error) {
	full, err := filepath.EvalSymlinks(filepath.Join(b.Root, filepath.FromSlash(rel)))
	if err != nil || !within(b.Root, full) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
	}

	info, err := os.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
	}

	if info.Size() > limit {
		return nil, fmt.Errorf("%s: %d KB, over the %d KB limit", rel, info.Size()>>kilobyte, limit>>kilobyte)
	}

	return os.ReadFile(full)
}

// child finds or adds the sub-folder name of a folder node.
func (n *Node) child(name, p string) *Node {
	if c := n.find(name); c != nil {
		return c
	}

	c := &Node{Name: name, Path: p, Dir: true}
	n.Children = append(n.Children, c)

	return c
}

// find finds the sub-folder name of a folder node; nil when absent.
func (n *Node) find(name string) *Node {
	for _, c := range n.Children {
		if c.Dir && c.Name == name {
			return c
		}
	}

	return nil
}

// sort orders a folder's children, folders first, then by name, and the
// sub-folders' too.
func (n *Node) sort() {
	slices.SortFunc(n.Children, func(x, y *Node) int {
		if x.Dir != y.Dir {
			if x.Dir {
				return -1
			}

			return 1
		}

		return cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})

	for _, c := range n.Children {
		if c.Dir {
			c.sort()
		}
	}
}

// Change sums up what a new scan changed from the previous one.
type Change struct {
	Added, Removed, Changed int
}

// Any reports whether anything changed.
func (c Change) Any() bool {
	return c.Added+c.Removed+c.Changed > 0
}

// diff compares two scans of the same folder by page path and digest.
func diff(before, after *Base) Change {
	var c Change

	for _, p := range after.Pages {
		old := before.page(p.Path)

		switch {
		case old == nil:
			c.Added++
		case old.Digest != p.Digest || old.Err != p.Err:
			c.Changed++
		}
	}

	if before != nil {
		for _, p := range before.Pages {
			if after.page(p.Path) == nil {
				c.Removed++
			}
		}
	}

	return c
}
