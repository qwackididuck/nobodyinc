package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const permissionEvent = `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`

func TestAutomationLevelAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantLevel  string
	}{
		{"on", `{"level":"auto"}`, http.StatusOK, "auto"},
		{"off", `{"level":"off"}`, http.StatusOK, "off"},
		{"unknown level", `{"level":"yolo"}`, http.StatusBadRequest, "off"},
		{"unknown field", `{"level":"auto","x":1}`, http.StatusBadRequest, "off"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newTestEnv(t)

			if rec := env.do(t, http.MethodPut, "/api/automation", tt.body); rec.Code != tt.wantStatus {
				t.Fatalf("PUT status = %d (%s)", rec.Code, rec.Body)
			}

			var got automationState
			decode(t, env.do(t, http.MethodGet, "/api/automation", ""), &got)

			if string(got.Level) != tt.wantLevel {
				t.Fatalf("level = %q, want %q", got.Level, tt.wantLevel)
			}
		})
	}
}

// With the mode on, a permission request is allowed at once and never queued.
func TestAutomaticModeAllowsPermissionRequests(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.do(t, http.MethodPut, "/api/automation", `{"level":"auto"}`)

	var decision hookDecision
	decode(t, env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", permissionEvent), &decision)

	if got := decision.Response.HookSpecificOutput.Decision.Behavior; got != "allow" {
		t.Fatalf("behavior = %q, want allow", got)
	}

	if n := len(env.approvals.Pending()); n != 0 {
		t.Fatalf("%d requests queued, want none", n)
	}
}

// Requests already waiting when the mode is switched on are allowed too.
func TestAutomaticModeAnswersWaitingRequests(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	answer := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answer <- env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", permissionEvent)
	}()

	waitForApproval(t, env)

	if rec := env.do(t, http.MethodPut, "/api/automation", `{"level":"auto"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s)", rec.Code, rec.Body)
	}

	var decision hookDecision
	decode(t, <-answer, &decision)

	if got := decision.Response.HookSpecificOutput.Decision.Behavior; got != "allow" {
		t.Fatalf("behavior = %q, want allow", got)
	}
}
