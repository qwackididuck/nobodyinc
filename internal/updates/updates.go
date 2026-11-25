// Package updates tells the human that a newer Nobody Inc release exists
// (ADR-0022, docs/updates.md).
//
// The launcher asks GitHub for the repository's latest release when it
// starts, and again when the human asks. The request is anonymous: a GET of
// a public URL with a generic User-Agent, nothing about the user or their
// projects. Nothing is downloaded or installed: the control room shows a
// banner that links to the release page.
//
// Only a build with a release version (vX.Y.Z) is compared. A development
// build (`dev`, or `git describe` output like v1.2.3-4-gabc-dirty) is
// never told to update.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// LatestReleaseURL is GitHub's API for the latest published release (drafts
// and prereleases excluded) of the repository.
const LatestReleaseURL = "https://api.github.com/repos/qwackididuck/nobodyinc/releases/latest"

// DefaultTimeout bounds one check.
const DefaultTimeout = 5 * time.Second

// maxBody bounds what is read from GitHub's answer.
const maxBody = 1 << 20

// Status is what is known about newer releases.
type Status struct {
	// Current is this build's version.
	Current string `json:"current"`
	// Latest is the latest release's tag; empty before a successful check.
	Latest string `json:"latest,omitempty"`
	// URL is the latest release's page.
	URL string `json:"url,omitempty"`
	// Available is true when Latest is newer than Current.
	Available bool `json:"available"`
	// CheckedAt is when the last check ended; nil before any check.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	// Error says why the last check failed.
	Error string `json:"error,omitempty"`
}

// Options configure a Checker; only Current is required.
type Options struct {
	// Current is this build's version.
	Current string
	// URL replaces LatestReleaseURL (tests).
	URL string
	// Client replaces the default client, whose timeout is DefaultTimeout.
	Client *http.Client
	// Now replaces time.Now (tests).
	Now func() time.Time
}

// Checker looks for newer releases and remembers the last result. It is safe
// for concurrent use.
type Checker struct {
	opts Options

	mu   sync.Mutex
	last Status
}

// New returns a Checker with the defaults filled in.
func New(opts Options) *Checker {
	if opts.URL == "" {
		opts.URL = LatestReleaseURL
	}

	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: DefaultTimeout}
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &Checker{opts: opts, last: Status{Current: opts.Current}}
}

// Status returns the result of the last check.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.last
}

// Check asks GitHub for the latest release, remembers the result and returns
// it. A failure is reported in Status.Error, not as an error: it must never
// get in the way of the launcher.
func (c *Checker) Check(ctx context.Context) Status {
	status := Status{Current: c.opts.Current}

	release, err := c.fetch(ctx)
	if err == nil {
		status.Latest = release.Tag
		status.URL = release.URL
		status.Available = newer(release.Tag, c.opts.Current)
	} else {
		status.Error = err.Error()
	}

	now := c.opts.Now()
	status.CheckedAt = &now

	c.mu.Lock()
	c.last = status
	c.mu.Unlock()

	return status
}

// release is the part of GitHub's answer that is used.
type release struct {
	Tag string `json:"tag_name"`
	URL string `json:"html_url"`
}

func (c *Checker) fetch(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.URL, http.NoBody)
	if err != nil {
		return release{}, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nobody-inc")

	resp, err := c.opts.Client.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// 404 also means a private repository: anonymous requests cannot see it.
		return release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}

	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
		return release{}, fmt.Errorf("read GitHub's answer: %w", err)
	}

	if r.Tag == "" {
		return release{}, errors.New("GitHub's answer has no release tag")
	}

	return r, nil
}

// releaseVersion matches a release tag: vX.Y.Z, nothing else.
var releaseVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// parse returns the numbers of a release version, false for anything else
// (dev builds, prereleases, git describe output).
func parse(version string) ([3]int, bool) {
	m := releaseVersion.FindStringSubmatch(version)
	if m == nil {
		return [3]int{}, false
	}

	var v [3]int

	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}

		v[i] = n
	}

	return v, true
}

// IsRelease reports whether version is a release version (vX.Y.Z), as
// opposed to a development build.
func IsRelease(version string) bool {
	_, ok := parse(version)

	return ok
}

// newer reports whether latest is a release newer than current. It is false
// when either is not a release version: a development build is never told
// to update.
func newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}

	c, ok := parse(current)
	if !ok {
		return false
	}

	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}

	return false
}
