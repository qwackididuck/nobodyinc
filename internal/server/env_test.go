package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/projectenv"
)

func newEnvServer(t *testing.T) (http.Handler, *projectenv.Store) {
	t.Helper()

	store, err := projectenv.Open(filepath.Join(t.TempDir(), projectenv.FileName))
	if err != nil {
		t.Fatal(err)
	}

	o := &org.Org{ProjectDir: t.TempDir()}

	return New(Deps{Logger: slog.New(slog.DiscardHandler), Org: o, Env: store}), store
}

func call(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestEnvAPI(t *testing.T) {
	t.Parallel()

	h, store := newEnvServer(t)

	rec := call(t, h, http.MethodPut, "/api/env/GITLAB_TOKEN", `{"value":"glpat-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}

	// The value is saved, and never comes back.
	if v, _ := store.Lookup("GITLAB_TOKEN"); v != "glpat-secret" {
		t.Fatalf("stored value = %q", v)
	}

	for _, path := range []string{"/api/env"} {
		rec = call(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "glpat-secret") {
			t.Fatalf("GET %s = %d %s: the value must not be returned", path, rec.Code, rec.Body)
		}

		if !strings.Contains(rec.Body.String(), `"GITLAB_TOKEN"`) {
			t.Fatalf("GET %s = %s: want the name", path, rec.Body)
		}
	}

	if strings.Contains(call(t, h, http.MethodPut, "/api/env/GITLAB_TOKEN", `{"value":"glpat-other"}`).Body.String(), "glpat-") {
		t.Fatal("PUT must not return a value either")
	}

	if rec = call(t, h, http.MethodDelete, "/api/env/GITLAB_TOKEN", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}

	if rec = call(t, h, http.MethodDelete, "/api/env/GITLAB_TOKEN", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second DELETE = %d, want 404", rec.Code)
	}
}

func TestEnvAPIRefusals(t *testing.T) {
	t.Parallel()

	h, _ := newEnvServer(t)

	tests := []struct{ name, path, body string }{
		{"reserved", "/api/env/NOBODY_AGENT", `{"value":"x"}`},
		{"parent session marker", "/api/env/CLAUDE_CODE_SESSION_ID", `{"value":"x"}`},
		{"bad name", "/api/env/1X", `{"value":"x"}`},
		{"empty value", "/api/env/TOKEN", `{"value":""}`},
		{"unknown field", "/api/env/TOKEN", `{"value":"x","other":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if rec := call(t, h, http.MethodPut, tt.path, tt.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("PUT %s = %d %s, want 400", tt.path, rec.Code, rec.Body)
			}
		})
	}
}

func TestEnvAPIWithoutStore(t *testing.T) {
	t.Parallel()

	h := New(Deps{Logger: slog.New(slog.DiscardHandler), Org: &org.Org{ProjectDir: t.TempDir()}})

	if rec := call(t, h, http.MethodGet, "/api/env", ""); rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET = %d, want 501", rec.Code)
	}
}
