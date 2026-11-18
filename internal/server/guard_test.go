package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuardCrossSite(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := GuardCrossSite([]string{"localhost:3000"}, ok)

	tests := []struct {
		name, method, path, origin, contentType, body string
		want                                          int
	}{
		{name: "read from anywhere", method: http.MethodGet, path: "/api/agents", origin: "https://evil.example", want: http.StatusTeapot},
		{name: "no Origin: not a browser", method: http.MethodPost, path: "/api/agents/a@dev/leave", want: http.StatusTeapot},
		{name: "control room", method: http.MethodPost, path: "/api/teams/dev/agents", origin: "http://localhost:3000", contentType: "application/json", body: "{}", want: http.StatusTeapot},
		{name: "same origin as the daemon", method: http.MethodDelete, path: "/api/agents/a@dev", origin: "http://127.0.0.1:7420", want: http.StatusTeapot},
		{name: "other site, no body", method: http.MethodPost, path: "/api/agents/a@dev/leave", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "other site, text/plain body", method: http.MethodPost, path: "/api/teams/dev/agents", origin: "https://evil.example", contentType: "text/plain", body: "{}", want: http.StatusForbidden},
		{name: "body that is not JSON", method: http.MethodPut, path: "/api/catalog/x", contentType: "text/plain", body: "{}", want: http.StatusUnsupportedMediaType},
		{name: "JSON with charset", method: http.MethodPut, path: "/api/catalog/x", contentType: "application/json; charset=utf-8", body: "{}", want: http.StatusTeapot},
		{name: "hooks are not guarded", method: http.MethodPost, path: "/internal/agents/a@dev/hook", contentType: "text/plain", body: "{}", want: http.StatusTeapot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "http://127.0.0.1:7420"+tt.path, strings.NewReader(tt.body))
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}
