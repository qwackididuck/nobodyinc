package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/projects"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// maxBodyBytes bounds request bodies: they are small.
const maxBodyBytes = 1 << 16

// projectView is a project as the API shows it (Project in
// api/openapi.yaml).
type projectView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	Missing    bool       `json:"missing"`
	Git        bool       `json:"git"`
	Trusted    bool       `json:"trusted"`
	Held       bool       `json:"held"`
	State      string     `json:"state"`
	LastOpened *time.Time `json:"last_opened,omitempty"`
}

func (l *Launcher) view(e projects.Entry) projectView {
	v := projectView{
		ID:      e.ID,
		Name:    e.Name(),
		Path:    e.Path,
		Missing: e.Missing(),
		Held:    l.leases.held(e.ID),
		State:   stateStopped,
	}

	if !v.Missing {
		v.Git = projects.IsGitRepo(e.Path)
		v.Trusted, _ = runtime.ProjectTrusted(e.Path) // unreadable Claude Code config: shown as untrusted
	}

	if p := l.existingProject(e.ID); p != nil {
		v.State = p.current().State
	}

	if !e.LastOpened.IsZero() {
		v.LastOpened = &e.LastOpened
	}

	return v
}

// listProjects serves GET /api/projects.
func (l *Launcher) listProjects(w http.ResponseWriter, _ *http.Request) {
	entries, err := l.registry.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	views := make([]projectView, 0, len(entries))
	for _, e := range entries {
		views = append(views, l.view(e))
	}

	writeJSON(w, http.StatusOK, views)
}

// opening is the body of POST /api/projects.
type opening struct {
	Path   string `json:"path"`
	Create bool   `json:"create"`
}

// openProject serves POST /api/projects: it makes a folder a known project,
// setting it up (and creating it) when needed.
func (l *Launcher) openProject(w http.ResponseWriter, r *http.Request) {
	var body opening

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %w", err))

		return
	}

	if !filepath.IsAbs(body.Path) {
		writeError(w, http.StatusBadRequest, errors.New("path must be absolute"))

		return
	}

	result, err := l.setUp(r, body)
	if err != nil {
		writeError(w, setUpStatus(err), err)

		return
	}

	entry, err := l.registry.Add(result.Dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated

		l.opts.Logger.Info("project set up", "project", entry.ID, "path", entry.Path,
			"git", result.Git, "trusted", result.Trusted)
	}

	writeJSON(w, status, l.view(entry))
}

func (l *Launcher) setUp(r *http.Request, body opening) (projects.SetUpResult, error) {
	if body.Create {
		return projects.Create(r.Context(), l.opts.Home, body.Path)
	}

	return projects.SetUp(l.opts.Home, body.Path)
}

func setUpStatus(err error) int {
	switch {
	case errors.Is(err, projects.ErrNotDirectory):
		return http.StatusBadRequest
	case errors.Is(err, projects.ErrExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// directoryListing is the answer of GET /api/fs/dirs (DirectoryListing in
// api/openapi.yaml).
type directoryListing struct {
	Path   string      `json:"path"`
	Parent string      `json:"parent,omitempty"`
	Home   string      `json:"home"`
	Dirs   []directory `json:"dirs"`
}

type directory struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Git     bool   `json:"git"`
	Nobody  bool   `json:"nobody"`
	Project string `json:"project,omitempty"`
}

// listDirs serves GET /api/fs/dirs: the sub-folders of a folder, to choose
// a project. Only names: nothing is read inside files.
func (l *Launcher) listDirs(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = home
	}

	if !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, errors.New("path must be absolute"))

		return
	}

	path = filepath.Clean(path)

	dirs, err := l.subFolders(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	listing := directoryListing{Path: path, Home: home, Dirs: dirs}
	if parent := filepath.Dir(path); parent != path {
		listing.Parent = parent
	}

	writeJSON(w, http.StatusOK, listing)
}

// subFolders lists the folders in path, hidden ones left out, sorted by
// name, each with what makes it a project candidate.
func (l *Launcher) subFolders(path string) ([]directory, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	known, err := l.knownPaths()
	if err != nil {
		return nil, err
	}

	dirs := []directory{}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}

		full := filepath.Join(path, e.Name())

		// os.Stat follows symbolic links: a link to a folder is a folder.
		if info, err := os.Stat(full); err != nil || !info.IsDir() { //nolint:gosec // browsing the machine's folders is the point
			continue
		}

		dirs = append(dirs, directory{
			Name:    e.Name(),
			Path:    full,
			Git:     projects.IsGitRepo(full),
			Nobody:  org.HasConfig(full),
			Project: known[full],
		})
	}

	return dirs, nil
}

// knownPaths maps the known projects' paths to their ids.
func (l *Launcher) knownPaths() (map[string]string, error) {
	entries, err := l.registry.List()
	if err != nil {
		return nil, err
	}

	known := make(map[string]string, len(entries))
	for _, e := range entries {
		known[e.Path] = e.ID
	}

	return known, nil
}
