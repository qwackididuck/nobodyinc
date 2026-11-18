package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDaemon answers hook events with a fixed decision and records acks.
type fakeDaemon struct {
	decision string
	acked    []string
	paths    []string
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.paths = append(f.paths, r.URL.EscapedPath())
	if strings.HasSuffix(r.URL.Path, "/ack") {
		var body struct {
			MessageIDs []string `json:"message_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		f.acked = append(f.acked, body.MessageIDs...)

		w.WriteHeader(http.StatusNoContent)

		return
	}

	io.WriteString(w, f.decision) //nolint:errcheck // test server
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		decision   string
		wantStdout string
		wantAcked  []string
	}{
		{
			name:       "mail to deliver: print then acknowledge",
			decision:   `{"response":{"decision":"block","reason":"read your mail"},"message_ids":["m1","m2"]}`,
			wantStdout: `{"decision":"block","reason":"read your mail"}` + "\n",
			wantAcked:  []string{"m1", "m2"},
		},
		{
			name:     "nothing to say: no output, no ack",
			decision: `{"message_ids":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			daemon := &fakeDaemon{decision: tt.decision}

			srv := httptest.NewServer(daemon)
			defer srv.Close()

			var out bytes.Buffer

			err := run(t.Context(), strings.NewReader(`{"hook_event_name":"Stop"}`), &out, config{agent: "archi@specs", daemonURL: srv.URL}, srv.Client())
			if err != nil {
				t.Fatal(err)
			}

			if out.String() != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", out.String(), tt.wantStdout)
			}

			if strings.Join(daemon.acked, ",") != strings.Join(tt.wantAcked, ",") {
				t.Fatalf("acked = %v, want %v", daemon.acked, tt.wantAcked)
			}

			if daemon.paths[0] != "/internal/agents/archi@specs/hook" {
				t.Fatalf("hook path = %s", daemon.paths[0])
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unknown agent"}`, http.StatusNotFound)
	}))
	t.Cleanup(failing.Close)

	tests := []struct {
		name string
		cfg  config
	}{
		{"agent not configured", config{daemonURL: failing.URL}},
		{"daemon refuses", config{agent: "ghost@dev", daemonURL: failing.URL}},
		{"daemon unreachable", config{agent: "archi@specs", daemonURL: "http://127.0.0.1:1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			if err := run(t.Context(), strings.NewReader(`{}`), &out, tt.cfg, failing.Client()); err == nil {
				t.Fatal("expected an error")
			}

			if out.Len() != 0 {
				t.Fatalf("nothing must be printed on error, got %q", out.String())
			}
		})
	}
}

func TestReportStatusLine(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotBody string
	)

	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.EscapedPath(), string(body)

		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(daemon.Close)

	report := `{"session_id":"s1","cost":{"total_cost_usd":0.25}}`
	cfg := config{agent: "dev1@dev", daemonURL: daemon.URL}

	if err := reportStatusLine(t.Context(), strings.NewReader(report), cfg, daemon.Client()); err != nil {
		t.Fatal(err)
	}

	if gotPath != "/internal/agents/dev1@dev/statusline" || gotBody != report {
		t.Fatalf("posted %q to %s, want the report to /internal/agents/dev1@dev/statusline", gotBody, gotPath)
	}

	if err := reportStatusLine(t.Context(), strings.NewReader(report), config{daemonURL: daemon.URL}, daemon.Client()); err == nil {
		t.Fatal("no error without NOBODY_AGENT")
	}
}
