package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	h := NewHandler("test", slog.New(slog.NewTextHandler(io.Discard, nil)))

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"health ok", http.MethodGet, "/healthz", http.StatusOK},
		{"health wrong method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{"unknown route", http.MethodGet, "/nope", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var got Health
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got != (Health{Status: "ok", Version: "test"}) {
				t.Fatalf("body = %+v", got)
			}
		})
	}
}
