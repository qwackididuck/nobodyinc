package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"patch", "v1.0.1", "v1.0.0", true},
		{"minor beats patch", "v1.1.0", "v1.0.9", true},
		{"major", "v2.0.0", "v1.9.9", true},
		{"numeric, not textual", "v1.10.0", "v1.9.0", true},
		{"same", "v1.2.3", "v1.2.3", false},
		{"older", "v1.2.3", "v1.2.4", false},
		{"dev build", "v9.0.0", "dev", false},
		{"git describe", "v9.0.0", "v1.2.3-4-gabc1234-dirty", false},
		{"commit only", "v9.0.0", "abc1234", false},
		{"empty current", "v9.0.0", "", false},
		{"prerelease tag", "v2.0.0-rc1", "v1.0.0", false},
		{"no v", "2.0.0", "v1.0.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := newer(tc.latest, tc.current); got != tc.want {
				t.Errorf("newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		current   string
		handler   http.HandlerFunc
		available bool
		latest    string
		errPart   string
	}{
		{
			name:    "newer release",
			current: "v1.0.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"https://example.test/r/v1.1.0"}`))
			},
			available: true,
			latest:    "v1.1.0",
		},
		{
			name:    "up to date",
			current: "v1.1.0",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"https://example.test/r/v1.1.0"}`))
			},
			latest: "v1.1.0",
		},
		{
			name:    "private repository",
			current: "v1.0.0",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
			errPart: "404",
		},
		{
			name:    "rate limited",
			current: "v1.0.0",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
			errPart: "403",
		},
		{
			name:    "not JSON",
			current: "v1.0.0",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
			errPart: "read GitHub's answer",
		},
		{
			name:    "no tag",
			current: "v1.0.0",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
			errPart: "no release tag",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var userAgent string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				userAgent = r.Header.Get("User-Agent")
				tc.handler(w, r)
			}))
			defer srv.Close()

			c := New(Options{Current: tc.current, URL: srv.URL, Now: func() time.Time { return now }})

			got := c.Check(context.Background())

			if got.Available != tc.available || got.Latest != tc.latest {
				t.Errorf("available=%v latest=%q, want %v %q", got.Available, got.Latest, tc.available, tc.latest)
			}

			if tc.errPart == "" && got.Error != "" || !strings.Contains(got.Error, tc.errPart) {
				t.Errorf("error = %q, want it to contain %q", got.Error, tc.errPart)
			}

			if got.CheckedAt == nil || !got.CheckedAt.Equal(now) {
				t.Errorf("checked_at = %v, want %v", got.CheckedAt, now)
			}

			if userAgent != "nobody-inc" {
				t.Errorf("User-Agent = %q, want a generic one", userAgent)
			}

			if c.Status() != got {
				t.Errorf("Status() = %+v, want the last check %+v", c.Status(), got)
			}
		})
	}
}

func TestCheckUnreachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more

	got := New(Options{Current: "v1.0.0", URL: url}).Check(context.Background())

	if got.Error == "" || got.Available {
		t.Errorf("got %+v, want an error and nothing available", got)
	}
}

func TestStatusBeforeAnyCheck(t *testing.T) {
	t.Parallel()

	got := New(Options{Current: "v1.0.0"}).Status()

	if got.CheckedAt != nil || got.Available || got.Current != "v1.0.0" {
		t.Errorf("got %+v, want only the current version", got)
	}
}
