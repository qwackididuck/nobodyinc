package projects

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

// registryFile lists the known projects, in the user's Nobody Inc directory.
const registryFile = "projects.yaml"

// ErrUnknownProject is returned for an id the registry does not hold.
var ErrUnknownProject = errors.New("unknown project")

// Entry is a known project.
type Entry struct {
	// ID is the project's identifier, the name of its state directory (ID).
	ID string `yaml:"id"`
	// Path is the project folder's absolute path.
	Path string `yaml:"path"`
	// LastOpened is when a window last opened it; zero when never.
	LastOpened time.Time `yaml:"last_opened,omitempty"`
}

// Name is how the project is shown: its folder's name.
func (e Entry) Name() string {
	return filepath.Base(e.Path)
}

// Missing reports whether the project's folder has disappeared (moved or
// deleted outside Nobody Inc). It stays listed until the human removes it.
func (e Entry) Missing() bool {
	info, err := os.Stat(e.Path)

	return err != nil || !info.IsDir()
}

// registryContent is the content of projects.yaml.
type registryContent struct {
	Projects []Entry `yaml:"projects"`
}

// Registry is the list of known projects, ~/.nobody/projects.yaml. Only the
// launcher writes it; every write replaces the file atomically. The file is
// read again for every operation: it is small, and a change made by hand is
// picked up at once.
type Registry struct {
	path string
	mu   sync.Mutex
}

// NewRegistry returns the registry kept in home.
func NewRegistry(home string) *Registry {
	return &Registry{path: filepath.Join(home, registryFile)}
}

// List returns the known projects, most recently opened first, then by
// name.
func (r *Registry) List() ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.read()
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(entries, func(a, b Entry) int {
		if c := b.LastOpened.Compare(a.LastOpened); c != 0 {
			return c
		}

		return strings.Compare(a.Name(), b.Name())
	})

	return entries, nil
}

// Get returns the project with this id.
func (r *Registry) Get(id string) (Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.read()
	if err != nil {
		return Entry{}, err
	}

	if i := indexOf(entries, id); i >= 0 {
		return entries[i], nil
	}

	return Entry{}, fmt.Errorf("%w: %s", ErrUnknownProject, id)
}

// Add makes the project at the absolute path projectDir known, and returns
// its entry. Adding a known project changes nothing.
func (r *Registry) Add(projectDir string) (Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.read()
	if err != nil {
		return Entry{}, err
	}

	entry := Entry{ID: ID(projectDir), Path: projectDir}
	if i := indexOf(entries, entry.ID); i >= 0 {
		return entries[i], nil
	}

	return entry, r.write(append(entries, entry))
}

// Touch records that a window opened the project at t.
func (r *Registry) Touch(id string, t time.Time) error {
	return r.update(id, func(entries []Entry, i int) []Entry {
		entries[i].LastOpened = t.UTC().Truncate(time.Second)

		return entries
	})
}

// Remove forgets the project. Nothing is deleted on disk.
func (r *Registry) Remove(id string) error {
	return r.update(id, func(entries []Entry, i int) []Entry {
		return slices.Delete(entries, i, i+1)
	})
}

// update applies change to the entry with this id and writes the result.
func (r *Registry) update(id string, change func(entries []Entry, i int) []Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.read()
	if err != nil {
		return err
	}

	i := indexOf(entries, id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrUnknownProject, id)
	}

	return r.write(change(entries, i))
}

// read returns the entries of the file; no file is no project.
func (r *Registry) read() ([]Entry, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var content registryContent
	if err := yaml.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.path, err)
	}

	return content.Projects, nil
}

// write replaces the file with these entries.
func (r *Registry) write(entries []Entry) error {
	data, err := yaml.Marshal(registryContent{Projects: entries})
	if err != nil {
		return err
	}

	header := "# Projects known to Nobody Inc, written by the launcher (docs/projects.md).\n"

	return atomicfile.Write(r.path, append([]byte(header), data...))
}

func indexOf(entries []Entry, id string) int {
	return slices.IndexFunc(entries, func(e Entry) bool { return e.ID == id })
}
