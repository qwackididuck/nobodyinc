package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/projects"
)

// What to do with a feature that is not finished when deleting its project.
const (
	featureMerge   = "merge"
	featureDiscard = "discard"
)

// refusalError stops a deletion because of the project's unfinished
// feature; nothing was deleted. It is the 409 answer of DELETE
// /api/projects/{project} (DeleteRefusal in api/openapi.yaml).
type refusalError struct {
	Message string        `json:"error"`
	Feature activeFeature `json:"feature"`
}

func (r *refusalError) Error() string { return r.Message }

// activeFeature is the part of a project daemon's feature we need.
type activeFeature struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
}

// deleteProject serves DELETE /api/projects/{project}: it removes Nobody Inc
// from the project, never its code (ADR-0014). A feature that is not
// finished is merged, discarded, or stops the deletion, as the human chose;
// then the project is stopped, and nobody may open it until it is gone.
func (l *Launcher) deleteProject(w http.ResponseWriter, r *http.Request) {
	entry, err := l.registry.Get(r.PathValue("project"))
	if errors.Is(err, projects.ErrUnknownProject) {
		writeError(w, http.StatusNotFound, err)

		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	choice := r.URL.Query().Get("feature")
	if choice != "" && choice != featureMerge && choice != featureDiscard {
		writeError(w, http.StatusBadRequest, fmt.Errorf("feature must be %q or %q", featureMerge, featureDiscard))

		return
	}

	err = l.removeProject(r.Context(), entry, r.URL.Query().Get("window"), choice)

	if refusal, ok := errors.AsType[*refusalError](err); ok {
		writeJSON(w, http.StatusConflict, refusal)

		return
	}

	switch {
	case errors.Is(err, errHeld), errors.Is(err, errDeleting):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		l.opts.Logger.Info("project deleted: Nobody Inc removed, the code kept", "project", entry.ID, "path", entry.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

// removeProject deletes the project for window (the window holding it, or
// none), in this order, so that a refusal changes nothing:
//
//  1. refused while another window holds the project (errHeld);
//  2. its unfinished feature is settled, or the deletion is refused
//     (*refusalError);
//  3. windows lose the project and may not open it again; it is stopped;
//  4. the teams' worktrees, .nobody/, the state directory and the registry
//     entry are removed.
func (l *Launcher) removeProject(ctx context.Context, entry projects.Entry, window, choice string) error {
	if l.leases.heldElsewhere(entry.ID, window) {
		return errHeld
	}

	p := l.project(entry)

	// Held throughout: the daemon cannot start or stop meanwhile.
	p.mu.Lock()
	defer p.mu.Unlock()

	if !entry.Missing() {
		if err := l.settleFeature(ctx, entry, choice, p.running()); err != nil {
			return err
		}
	}

	if err := l.leases.startDeleting(entry.ID, window); err != nil {
		return err
	}
	defer l.leases.finishDeleting(entry.ID)

	p.stopLocked()

	if !entry.Missing() {
		if err := projects.RemoveWorktrees(ctx, l.opts.Home, entry.Path); err != nil {
			return err
		}
	}

	if err := projects.Forget(l.opts.Home, entry.Path); err != nil {
		return err
	}

	if err := l.registry.Remove(entry.ID); err != nil {
		return err
	}

	l.mu.Lock()
	delete(l.projects, entry.ID)
	l.mu.Unlock()

	return nil
}

// settleFeature deals with the project's active feature, if it has one: a
// refusal when the human has not chosen yet, a forced finish (ADR-0007) for
// "merge", nothing for "discard" (its worktrees are removed afterwards, its
// branches stay).
//
// The project's state is in its daemon's database. The running daemon
// answers when there is one; otherwise the launcher runs the daemon without
// its agents (-start=false) to read the feature and finish it, and stops it
// before returning (the daemon waits for the feature's clean-up before it
// exits).
func (l *Launcher) settleFeature(ctx context.Context, entry projects.Entry, choice string, running *process) error {
	proc := running
	if proc == nil {
		db := filepath.Join(projects.StateDir(l.opts.Home, entry.Path), "nobody.db")
		if _, err := os.Stat(db); errors.Is(err, os.ErrNotExist) {
			return nil // never opened: no feature
		}

		var err error
		if proc, err = startProcess(ctx, l.processConfig(entry.ID, entry.Path, "-start=false")); err != nil {
			return fmt.Errorf("read the project's features: %w", err)
		}
		defer proc.stop()
	}

	feature, active, err := findActiveFeature(ctx, proc)
	if err != nil || !active {
		return err
	}

	switch choice {
	case featureDiscard:
		return nil
	case featureMerge:
		return finishFeature(ctx, proc, feature)
	default:
		return &refusalError{
			Message: fmt.Sprintf("the feature %q is not finished: merge it as it is, or delete it anyway", feature.Title),
			Feature: feature,
		}
	}
}

// findActiveFeature returns the project's active feature, and whether there
// is one.
func findActiveFeature(ctx context.Context, proc *process) (activeFeature, bool, error) {
	//nolint:gosec // our project daemon, on loopback
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proc.url.JoinPath("/api/features").String(), http.NoBody)
	if err != nil {
		return activeFeature{}, false, err
	}

	res, err := http.DefaultClient.Do(req) //nolint:gosec // our project daemon, on loopback
	if err != nil {
		return activeFeature{}, false, err
	}
	defer res.Body.Close()

	var features []struct {
		activeFeature

		Active bool `json:"active"`
	}

	if err := json.NewDecoder(res.Body).Decode(&features); err != nil {
		return activeFeature{}, false, fmt.Errorf("read the project's features: %w", err)
	}

	for _, f := range features {
		if f.Active {
			return f.activeFeature, true, nil
		}
	}

	return activeFeature{}, false, nil
}

// finishFeature finishes the feature anyway (its unfinished tickets
// canceled). When the daemon refuses (a conflict, uncommitted changes in the
// project...), the deletion is refused with its reason.
func finishFeature(ctx context.Context, proc *process, feature activeFeature) error {
	url := proc.url.JoinPath("/api/features", feature.ID, "finish").String()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"force":true}`)) //nolint:gosec // loopback
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req) //nolint:gosec // our project daemon, on loopback
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		return nil
	}

	reason := res.Status

	var body errorBody
	if json.NewDecoder(res.Body).Decode(&body) == nil && body.Error != "" {
		reason = body.Error
	}

	return &refusalError{Message: "the feature could not be merged, nothing was deleted: " + reason, Feature: feature}
}
