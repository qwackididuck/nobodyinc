package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/settings"
)

func TestListMessagesFilters(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	for i, m := range []messaging.Message{
		{ID: "m1", From: org.HumanAddress, To: "dev1@dev"},
		{ID: "m2", From: "dev1@dev", To: org.HumanAddress},
		{ID: "m3", From: "dev1@dev", To: "archi@specs"},
	} {
		m.Conversation, m.Act, m.Priority, m.SentAt = m.ID, messaging.ActInform, messaging.Normal, t0.Add(time.Duration(i)*time.Minute)
		if err := env.store.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	if err := env.store.MarkDelivered(ctx, "archi@specs", []string{"m3"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query string
		code  int
		want  []string
	}{
		{"", http.StatusOK, []string{"m1", "m2", "m3"}},
		{"?with=dev1@dev", http.StatusOK, []string{"m1", "m2"}},
		{"?scope=agents", http.StatusOK, []string{"m3"}},
		{"?scope=everyone", http.StatusBadRequest, nil},
		{"?with=dev1@dev&scope=agents", http.StatusBadRequest, nil},
	}

	for _, tt := range tests {
		t.Run("query "+tt.query, func(t *testing.T) {
			t.Parallel()

			rec := env.do(t, http.MethodGet, "/api/messages"+tt.query, "")
			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.code, rec.Body)
			}

			if tt.code != http.StatusOK {
				return
			}

			var got []messaging.Message
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d messages, want %v", len(got), tt.want)
			}

			for i, id := range tt.want {
				if got[i].ID != id {
					t.Fatalf("message %d = %s, want %v", i, got[i].ID, tt.want)
				}
			}
		})
	}
}

func TestEmptyTheWastebasket(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	for i, m := range []messaging.Message{
		{ID: "a1", Conversation: "c", From: "dev1@dev", To: "archi@specs", Act: messaging.ActQuery},
		{ID: "a2", Conversation: "c", From: "archi@specs", To: "dev1@dev", Act: messaging.ActInform},
	} {
		m.Priority, m.SentAt = messaging.Normal, t0.Add(time.Duration(i)*time.Minute)
		if err := env.store.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}

		if err := env.store.MarkDelivered(ctx, m.To, []string{m.ID}, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	if rec := env.do(t, http.MethodDelete, "/api/messages", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("without scope: status = %d, want 400", rec.Code)
	}

	rec := env.do(t, http.MethodDelete, "/api/messages?scope=agents", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var got deletedMessages
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if len(got.IDs) != 2 {
		t.Fatalf("deleted %v, want both messages of the finished conversation", got.IDs)
	}
}

// settingsEnv is a router with settings, launch information and an MCP
// registry.
func settingsEnv(t *testing.T, live *settings.Live) testEnv {
	t.Helper()

	o := &org.Org{
		ProjectDir: "/work/shop",
		MCP: map[string]mcpservers.Server{
			"gitlab": {Scope: mcpservers.ScopeProject, Config: mcpservers.Config{
				"command": "gitlab-mcp", "args": []any{"--token", "glpat-literal"},
				"env": map[string]any{"GITLAB_TOKEN": "${GITLAB_TOKEN}", "GITLAB_URL": "${GITLAB_URL:-https://gitlab.com}"}, //nolint:gosec // references, not credentials
			}},
			"ragflow": {Scope: mcpservers.ScopeUser, Config: mcpservers.Config{
				"type": "http", "url": "https://me@rag.example/mcp?key=literal",
				"headers": map[string]any{"Authorization": "Bearer ${RAGFLOW_KEY}"},
			}},
		},
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
			{Name: "dev1", Definition: catalog.Definition{MCPExclude: []string{"gitlab", "ragflow/delete_document"}}}, //nolint:modernize // clearer with the field named
		}}},
	}
	env := map[string]string{"GITLAB_TOKEN": "secret", "GITLAB_URL": ""}

	return testEnv{handler: New(Deps{
		Logger:   slog.New(slog.DiscardHandler),
		Org:      o,
		Origins:  []string{"localhost:3000"},
		Settings: live,
		Info:     DaemonInfo{ProjectID: "a3f9c1", StateDir: "/home/me/.nobody/projects/a3f9c1", Address: "127.0.0.1:7420", ClaudePath: "claude", StartAgents: true},
		LookupEnv: func(name string) (string, bool) {
			v, ok := env[name]

			return v, ok
		},
	})}
}

func TestSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		live     *settings.Live
		method   string
		body     string
		code     int
		wantProt string
	}{
		{"read", settings.NewLive("", settings.Defaults()), http.MethodGet, "", http.StatusOK, settings.ProtectBlock},
		{"change to warn", settings.NewLive("", settings.Defaults()), http.MethodPut, `{"config_protection":"warn"}`, http.StatusOK, settings.ProtectWarn},
		{"invalid value", settings.NewLive("", settings.Defaults()), http.MethodPut, `{"config_protection":"maybe"}`, http.StatusBadRequest, ""},
		{"unknown field", settings.NewLive("", settings.Defaults()), http.MethodPut, `{"origins":["evil.example"]}`, http.StatusBadRequest, ""},
		{"no settings to change", nil, http.MethodPut, `{"config_protection":"warn"}`, http.StatusNotImplemented, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := settingsEnv(t, tt.live).do(t, tt.method, "/api/settings", tt.body)
			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.code, rec.Body)
			}

			if tt.code != http.StatusOK {
				return
			}

			var got settingsView
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			want := settingsView{
				ProjectDir: "/work/shop", ProjectID: "a3f9c1", StateDir: "/home/me/.nobody/projects/a3f9c1",
				Address: "127.0.0.1:7420", Origins: []string{"localhost:3000"}, ClaudePath: "claude", StartAgents: true,
				ConfigProtection: tt.wantProt,
			}
			if got.ConfigProtection != want.ConfigProtection || got.ProjectDir != want.ProjectDir || got.ProjectID != want.ProjectID ||
				got.Address != want.Address || len(got.Origins) != 1 || !got.StartAgents {
				t.Fatalf("settings = %+v, want %+v", got, want)
			}
		})
	}
}

func TestListMCPServers(t *testing.T) {
	t.Parallel()

	rec := settingsEnv(t, nil).do(t, http.MethodGet, "/api/mcp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	for _, secret := range []string{"secret", "Bearer", "glpat-literal", "literal", "me@"} {
		if body := rec.Body.String(); strings.Contains(body, secret) {
			t.Fatalf("%q leaked: %s", secret, body)
		}
	}

	var got []mcpServerView
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	want := []mcpServerView{
		{Name: "gitlab", Source: mcpservers.ScopeProject, Type: "stdio", Target: "gitlab-mcp",
			Env: []envVarState{{Name: "GITLAB_TOKEN", Set: true}, {Name: "GITLAB_URL", Set: false}}, ExcludedBy: []string{"dev1@dev"}},
		{Name: "ragflow", Source: mcpservers.ScopeUser, Type: "http", Target: "https://rag.example/mcp",
			Env: []envVarState{{Name: "RAGFLOW_KEY", Set: false}}, ExcludedBy: []string{}},
	}

	gotJSON, _ := json.Marshal(got)   //nolint:errchkjson // plain structs
	wantJSON, _ := json.Marshal(want) //nolint:errchkjson // plain structs

	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("servers =\n%s\nwant\n%s", gotJSON, wantJSON)
	}
}
