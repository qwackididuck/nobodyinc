package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

type testEnv struct {
	handler   http.Handler
	store     *store.Store
	statuses  *runtime.Statuses
	approvals *approval.Queue
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	return newTestEnvWithWait(t, 0)
}

// newTestEnvWithWait is newTestEnv with a custom approval wait (0: default).
func newTestEnvWithWait(t *testing.T, approvalWait time.Duration) testEnv {
	t.Helper()

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := &org.Org{
		ProjectDir: t.TempDir(),
		Teams: []org.Team{
			{ID: "specs", Agents: []org.Agent{{Name: "archi", Role: "Architect"}}, Block: []string{"dev"}},
			{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}},
		},
	}
	statuses := runtime.NewStatuses()
	approvals := approval.NewQueue()
	tickets := &work.Service{Store: st, Org: o}

	budgets, err := budget.OpenBook(o.ProjectDir, o, nil)
	if err != nil {
		t.Fatal(err)
	}

	h := New(Deps{
		Version:      "test",
		Logger:       slog.New(slog.DiscardHandler),
		Org:          o,
		Store:        st,
		Messages:     &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}},
		Statuses:     statuses,
		Approvals:    approvals,
		Tickets:      tickets,
		ApprovalWait: approvalWait,
		Spend:        &budget.Meter{Store: st, Attribute: ticketInProgress(st)},
		Budgets:      budgets,
	})

	return testEnv{handler: h, store: st, statuses: statuses, approvals: approvals}
}

func (e testEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json") // as the control room sends it

	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	return rec
}

func TestHealth(t *testing.T) {
	t.Parallel()

	rec := newTestEnv(t).do(t, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"version":"test"`)) {
		t.Fatalf("GET /healthz = %d %s", rec.Code, rec.Body)
	}
}

func TestSendMessageAsHuman(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"valid", `{"to":"archi@specs","act":"request","priority":"normal","subject":"Design the API"}`, http.StatusCreated},
		{"unknown agent", `{"to":"ghost@specs","act":"request","priority":"normal","subject":"x"}`, http.StatusBadRequest},
		{"unknown act", `{"to":"archi@specs","act":"yell","priority":"normal","subject":"x"}`, http.StatusBadRequest},
		{"unknown field", `{"to":"archi@specs","act":"inform","priority":"normal","subject":"x","cc":"all"}`, http.StatusBadRequest},
		{"not json", `hello`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newTestEnv(t).do(t, http.MethodPost, "/api/messages", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestListAgentsTeamsAndMessages(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.statuses.Set("archi@specs", runtime.StatusIdle)
	env.do(t, http.MethodPost, "/api/messages", `{"to":"archi@specs","act":"inform","priority":"normal","subject":"hello"}`)

	var agents []agentView
	decode(t, env.do(t, http.MethodGet, "/api/agents", ""), &agents)

	if len(agents) != 2 || agents[0].Address != "archi@specs" || agents[0].Status != "idle" || agents[1].Status != "stopped" {
		t.Fatalf("agents = %+v", agents)
	}

	var teams []teamView
	decode(t, env.do(t, http.MethodGet, "/api/teams", ""), &teams)

	if len(teams) != 2 || teams[0].ID != "specs" || teams[1].ID != "dev" ||
		!slices.Equal(teams[0].Block, []string{"dev"}) || teams[1].Block == nil {
		t.Fatalf("teams = %+v, want specs blocking dev, and dev's block list empty, not null", teams)
	}

	var messages []messaging.Message
	decode(t, env.do(t, http.MethodGet, "/api/messages", ""), &messages)

	if len(messages) != 1 || messages[0].From != org.HumanAddress || messages[0].Subject != "hello" {
		t.Fatalf("messages = %+v", messages)
	}

	if rec := env.do(t, http.MethodGet, "/api/messages?limit=0", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 status = %d", rec.Code)
	}
}

func TestHookDeliveryAndAck(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.do(t, http.MethodPost, "/api/messages", `{"to":"archi@specs","act":"query","priority":"normal","subject":"Which DB?"}`)

	// Stop: the mail is handed over and the agent keeps working.
	var decision hookDecision
	decode(t, env.do(t, http.MethodPost, "/internal/agents/archi@specs/hook", `{"hook_event_name":"Stop","extra":"ignored"}`), &decision)

	if decision.Response == nil || decision.Response.Decision != "block" || len(decision.MessageIDs) != 1 {
		t.Fatalf("decision = %+v", decision)
	}

	if got := env.statuses.Get("archi@specs"); got != runtime.StatusWorking {
		t.Fatalf("status after delivery = %s, want working", got)
	}

	// Without ack the message stays pending (at-least-once).
	decode(t, env.do(t, http.MethodPost, "/internal/agents/archi@specs/hook", `{"hook_event_name":"Stop"}`), &decision)

	if len(decision.MessageIDs) != 1 {
		t.Fatalf("unacknowledged message should be delivered again, got %+v", decision)
	}

	ack, err := json.Marshal(hookAck{MessageIDs: decision.MessageIDs})
	if err != nil {
		t.Fatal(err)
	}

	if rec := env.do(t, http.MethodPost, "/internal/agents/archi@specs/hook/ack", string(ack)); rec.Code != http.StatusNoContent {
		t.Fatalf("ack status = %d (%s)", rec.Code, rec.Body)
	}

	// Stop again: nothing left, the agent goes idle.
	decision = hookDecision{}
	decode(t, env.do(t, http.MethodPost, "/internal/agents/archi@specs/hook", `{"hook_event_name":"Stop"}`), &decision)

	if decision.Response != nil || len(decision.MessageIDs) != 0 {
		t.Fatalf("expected nothing to deliver, got %+v", decision)
	}

	if got := env.statuses.Get("archi@specs"); got != runtime.StatusIdle {
		t.Fatalf("status = %s, want idle", got)
	}
}

func TestHookUnknownAgent(t *testing.T) {
	t.Parallel()

	rec := newTestEnv(t).do(t, http.MethodPost, "/internal/agents/ghost@dev/hook", `{"hook_event_name":"Stop"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPermissionRequestWaitsForTheHuman(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		decision     string
		wantBehavior string
	}{
		{"allowed from the UI", `{"decision":"allow"}`, "allow"},
		{"denied from the UI", `{"decision":"deny","message":"stay in the sandbox"}`, "deny"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newTestEnv(t)

			answer := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				answer <- env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook",
					`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
			}()

			pending := waitForApproval(t, env)
			if pending.Agent != "dev1@dev" || pending.ToolName != "Bash" || !bytes.Contains(pending.ToolInput, []byte("rm -rf")) {
				t.Fatalf("pending approval = %+v", pending)
			}

			if got := env.statuses.Get("dev1@dev"); got != runtime.StatusAwaitingApproval {
				t.Fatalf("status while waiting = %s", got)
			}

			if rec := env.do(t, http.MethodPost, "/api/approvals/"+pending.ID, tt.decision); rec.Code != http.StatusNoContent {
				t.Fatalf("decide status = %d (%s)", rec.Code, rec.Body)
			}

			var decision hookDecision
			decode(t, <-answer, &decision)

			got := decision.Response.HookSpecificOutput
			if got.HookEventName != "PermissionRequest" || got.Decision.Behavior != tt.wantBehavior {
				t.Fatalf("hook answer = %+v / %+v", got, got.Decision)
			}

			if env.statuses.Get("dev1@dev") != runtime.StatusWorking {
				t.Fatal("the agent should be working again after the decision")
			}
		})
	}
}

// A prompt's suggestions (switch to auto mode…) reach the queue, and the one
// the human picks goes back to Claude Code as an update with the allow.
func TestPermissionRequestSuggestions(t *testing.T) {
	t.Parallel()

	const event = `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"},` +
		`"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls"}],"behavior":"allow","destination":"localSettings"},` +
		`{"type":"setMode","mode":"auto","destination":"session"}]}`

	tests := []struct {
		name        string
		decision    string
		wantUpdates int
		wantMode    string
		wantStatus  int
	}{
		{"plain allow", `{"decision":"allow"}`, 0, "", http.StatusNoContent},
		{"allow and switch to auto", `{"decision":"allow","suggestion":1}`, 1, "auto", http.StatusNoContent},
		{"index out of range", `{"decision":"allow","suggestion":2}`, 0, "", http.StatusBadRequest},
		{"suggestion with a deny", `{"decision":"deny","suggestion":0}`, 0, "", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newTestEnv(t)

			answer := make(chan *httptest.ResponseRecorder, 1)
			go func() { answer <- env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", event) }()

			pending := waitForApproval(t, env)
			if len(pending.Suggestions) != 2 {
				t.Fatalf("suggestions = %d, want 2", len(pending.Suggestions))
			}

			rec := env.do(t, http.MethodPost, "/api/approvals/"+pending.ID, tt.decision)
			if rec.Code != tt.wantStatus {
				t.Fatalf("decide status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body)
			}

			if tt.wantStatus != http.StatusNoContent {
				// Still pending: settle it so the hook call returns.
				env.do(t, http.MethodPost, "/api/approvals/"+pending.ID, `{"decision":"allow"}`)
			}

			var decision hookDecision
			decode(t, <-answer, &decision)

			got := decision.Response.HookSpecificOutput.Decision.UpdatedPermissions
			if len(got) != tt.wantUpdates {
				t.Fatalf("updates = %s, want %d", got, tt.wantUpdates)
			}

			if tt.wantMode != "" && !bytes.Contains(got[0], []byte(`"mode":"`+tt.wantMode+`"`)) {
				t.Fatalf("update = %s, want mode %s", got[0], tt.wantMode)
			}
		})
	}
}

// A permission the human answers in the agent's terminal leaves the queue as
// soon as the agent's next events show it.
func TestPermissionAnsweredInTheTerminalLeavesTheQueue(t *testing.T) {
	t.Parallel()

	const request = `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`

	tests := []struct {
		name      string
		event     string
		withdrawn bool
	}{
		{"the same tool ran", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`, true},
		{"another tool ran", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"pwd"}}`, false},
		{"the turn ended", `{"hook_event_name":"Stop"}`, true},
		{"a new prompt", `{"hook_event_name":"UserPromptSubmit"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newTestEnv(t)

			answer := make(chan *httptest.ResponseRecorder, 1)
			go func() { answer <- env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", request) }()

			pending := waitForApproval(t, env)
			env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", tt.event)

			if !tt.withdrawn {
				if len(env.approvals.Pending()) != 1 {
					t.Fatal("the request must still be pending")
				}

				env.do(t, http.MethodPost, "/api/approvals/"+pending.ID, `{"decision":"deny"}`)
			}

			var decision hookDecision
			decode(t, <-answer, &decision)

			if tt.withdrawn && (decision.Response != nil || len(env.approvals.Pending()) != 0) {
				t.Fatalf("the request must be withdrawn without an answer, got %+v", decision.Response)
			}
		})
	}
}

func TestPermissionRequestWithoutDecisionFallsBackToTerminal(t *testing.T) {
	t.Parallel()

	env := newTestEnvWithWait(t, 20*time.Millisecond)

	var decision hookDecision
	decode(t, env.do(t, http.MethodPost, "/internal/agents/dev1@dev/hook", `{"hook_event_name":"PermissionRequest","tool_name":"Edit"}`), &decision)

	if decision.Response != nil {
		t.Fatalf("without a decision nothing must be answered, got %+v", decision.Response)
	}

	if len(env.approvals.Pending()) != 0 {
		t.Fatal("an abandoned request must leave the queue")
	}
}

func TestDecideApprovalErrors(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	if rec := env.do(t, http.MethodPost, "/api/approvals/nope", `{"decision":"allow"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown approval: status = %d", rec.Code)
	}

	if rec := env.do(t, http.MethodPost, "/api/approvals/nope", `{"decision":"maybe"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid decision: status = %d", rec.Code)
	}
}

func TestTicketsAPI(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	rec := env.do(t, http.MethodPost, "/api/tickets", `{"title":"Build the order API","team":"dev","acceptance_criteria":"POST /orders works"}`)

	var created work.Ticket

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", rec.Code, rec.Body)
	}

	decode(t, rec, &created)

	if created.ID != "T-1" || created.CreatedBy != org.HumanAddress {
		t.Fatalf("created = %+v", created)
	}

	if rec := env.do(t, http.MethodPost, "/api/tickets/T-1/comments", `{"body":"Start with the happy path"}`); rec.Code != http.StatusCreated {
		t.Fatalf("comment status = %d (%s)", rec.Code, rec.Body)
	}

	var details ticketDetails
	decode(t, env.do(t, http.MethodGet, "/api/tickets/T-1", ""), &details)

	if details.Ticket.Title != "Build the order API" || len(details.Comments) != 1 || details.Deliverables == nil || details.Messages == nil {
		t.Fatalf("details = %+v", details)
	}

	var list []work.Ticket
	decode(t, env.do(t, http.MethodGet, "/api/tickets?team=dev&status=open", ""), &list)

	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}

	var canceled work.Ticket
	decode(t, env.do(t, http.MethodPost, "/api/tickets/T-1/status", `{"status":"canceled"}`), &canceled)

	if canceled.Status != work.StatusCanceled {
		t.Fatalf("status = %s", canceled.Status)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/tickets/T-9", "", http.StatusNotFound},
		{http.MethodPost, "/api/tickets", `{"title":"x","team":"ghosts"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/tickets/T-1/status", `{"status":"done"}`, http.StatusConflict},
	} {
		if rec := env.do(t, tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Fatalf("%s %s = %d, want %d (%s)", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
	}
}

func waitForApproval(t *testing.T, env testEnv) approval.Request {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var pending []approval.Request
		decode(t, env.do(t, http.MethodGet, "/api/approvals", ""), &pending)

		if len(pending) == 1 {
			return pending[0]
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("the permission request never reached the approval queue")

	return approval.Request{}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()

	if rec.Code >= 300 {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body)
	}

	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
}

func TestFeaturesAPI(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	rec := env.do(t, http.MethodPost, "/api/features", `{"title":"Add the order API","base_branch":"main"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start feature: %d (%s)", rec.Code, rec.Body)
	}

	var started work.Feature
	decode(t, rec, &started)

	if started.ID != "F-1" || !started.Active {
		t.Fatalf("started = %+v", started)
	}

	var features []work.Feature
	decode(t, env.do(t, http.MethodGet, "/api/features", ""), &features)

	if len(features) != 1 || !features[0].Active {
		t.Fatalf("features = %+v", features)
	}

	var tk work.Ticket
	decode(t, env.do(t, http.MethodPost, "/api/tickets", `{"title":"Build it","team":"dev"}`), &tk)

	if tk.Feature != "F-1" {
		t.Fatalf("a new ticket belongs to the active feature, got %q", tk.Feature)
	}

	if rec := env.do(t, http.MethodPost, "/api/features", `{"title":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a feature without title: status = %d", rec.Code)
	}
}
