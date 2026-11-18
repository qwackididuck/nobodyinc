// Package server exposes the nobodyd HTTP API described in api/openapi.yaml.
//
// Routes are grouped by audience:
//
//   - api.go:       /api/*           for the supervision UI and the human operator;
//   - approvals.go: /api/approvals*  permission requests waiting for the human;
//   - tickets.go:   /api/tickets*    tickets, as seen and handled by the human;
//   - features.go:  /api/features    features and their git branches;
//   - events.go:    /api/events      changes pushed to the control room (SSE);
//   - settings.go:  /api/settings    how the daemon runs;
//   - mcp.go:       /api/mcp*        MCP servers (ADR-0010);
//   - skills.go:    /api/skills*     skills (ADR-0011);
//   - schedules.go: /api/schedule*   scheduled tasks (ADR-0012);
//   - budgets.go:   /api/spend       costs and budgets (ADR-0013), and the
//     agents' statusLine reports;
//   - hooks.go:     /internal/*      for nobody-hook, called by Claude Code hooks.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/skills"
	"github.com/qwackididuck/nobodyinc/internal/staffing"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Deps are the services the HTTP layer relies on.
type Deps struct {
	Version  string
	Logger   *slog.Logger
	Org      org.Source
	Store    *store.Store
	Messages *messaging.Service
	Tickets  *work.Service
	Statuses *runtime.Statuses
	// Approvals queues the agents' permission requests for the human.
	Approvals *approval.Queue
	// ApprovalWait overrides how long a permission request waits for the
	// human before falling back to the agent's terminal (tests only).
	ApprovalWait time.Duration
	// Agents starts agent sessions; nil disables POST /api/agents/{address}/start.
	Agents AgentStarter
	// Staff recruits, edits, puts on leave and dismisses agents; Catalog
	// lists and saves templates. Nil disables those endpoints (staff.go).
	Staff   *staffing.Service
	Catalog *catalog.Catalog
	// Extra mounts additional handlers (MCP endpoint, terminals) by pattern.
	Extra map[string]http.Handler
	// Origins are the browser origins (host patterns, e.g. "localhost:3000")
	// allowed to change things through the API besides the daemon itself
	// (guard.go).
	Origins []string
	// Events is streamed by GET /api/events; nil disables it.
	Events *events.Bus
	// Heartbeat overrides how often an idle event stream sends a comment
	// (tests only).
	Heartbeat time.Duration
	// Settings are the user's settings, changed by PUT /api/settings; nil
	// makes them read-only (empty).
	Settings *settings.Live
	// Info describes how the daemon was launched (GET /api/settings).
	Info DaemonInfo
	// LookupEnv tells whether an environment variable is set
	// (GET /api/mcp); nil means os.LookupEnv.
	LookupEnv func(string) (string, bool)
	// BuiltinSkills is the daemon's list of Claude Code's built-in skills
	// (GET /api/skills); nil lists none.
	BuiltinSkills *skills.Builtin
	// Schedules backs the scheduled tasks (schedules.go); nil answers 501.
	Schedules Schedules
	// Spend turns the agents' statusLine reports into spend records
	// (budgets.go, ADR-0013); nil records nothing.
	Spend *budget.Meter
	// Budgets holds .nobody/budgets.yaml; nil answers 501 on /api/budgets
	// and leaves the gauges of /api/spend empty.
	Budgets *budget.Book
	// BudgetStop decides the Stop of an agent a budget may hold: the
	// response to give it, and whether it is held (hooks.go); nil holds
	// nobody.
	BudgetStop func(ctx context.Context, address string) (*hook.Response, bool, error)
	// BudgetOffice backs the budget requests and cap proposals
	// (budgets.go); nil answers 501.
	BudgetOffice BudgetOffice
	// SessionStarted is told about an agent's SessionStart hook: it returns
	// the context to add for a session resumed because the project opened
	// again, and the status the agent settles in ("" for unchanged; idle or
	// budget_hold for a silent resume, ADR-0015); nil adds nothing.
	SessionStarted func(address string) (string, runtime.Status)
}

// AgentStarter launches an agent's session and tells whether a change waits
// for it to end its turn (implemented by runtime.Supervisor).
type AgentStarter interface {
	Start(ctx context.Context, address string) error
	Pending(address string) bool
}

// Health is the liveness payload returned by GET /healthz.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type server struct {
	Deps
}

// New returns the router serving the daemon API.
func New(d Deps) http.Handler {
	s := &server{Deps: d}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)

	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("POST /api/agents/{address}/start", s.startAgent)
	mux.HandleFunc("DELETE /api/agents/{address}", s.dismissAgent)
	mux.HandleFunc("GET /api/agents/{address}/definition", s.getAgentDefinition)
	mux.HandleFunc("PUT /api/agents/{address}/definition", s.editAgentDefinition)
	mux.HandleFunc("POST /api/agents/{address}/leave", s.putAgentOnLeave)
	mux.HandleFunc("POST /api/agents/{address}/return", s.returnAgentFromLeave)
	mux.HandleFunc("GET /api/teams", s.listTeams)
	mux.HandleFunc("POST /api/teams", s.createTeam)
	mux.HandleFunc("PUT /api/teams/{team}", s.updateTeam)
	mux.HandleFunc("DELETE /api/teams/{team}", s.deleteTeam)
	mux.HandleFunc("POST /api/teams/{team}/agents", s.recruitAgent)
	mux.HandleFunc("GET /api/project", s.getProject)
	mux.HandleFunc("PUT /api/project", s.updateProject)
	mux.HandleFunc("GET /api/catalog", s.listTemplates)
	mux.HandleFunc("GET /api/catalog/{name}", s.getTemplate)
	mux.HandleFunc("PUT /api/catalog/{name}", s.saveTemplate)
	mux.HandleFunc("DELETE /api/catalog/{name}", s.deleteTemplate)
	mux.HandleFunc("GET /api/messages", s.listMessages)
	mux.HandleFunc("POST /api/messages", s.sendMessageAsHuman)
	mux.HandleFunc("DELETE /api/messages", s.deleteFinishedAgentConversations)
	mux.HandleFunc("GET /api/features", s.listFeatures)
	mux.HandleFunc("POST /api/features", s.startFeature)
	mux.HandleFunc("POST /api/features/{id}/start", s.startProposedFeature)
	mux.HandleFunc("POST /api/features/{id}/refuse", s.refuseProposedFeature)
	mux.HandleFunc("GET /api/features/{id}/review", s.reviewFeature)
	mux.HandleFunc("POST /api/features/{id}/finish", s.finishFeature)
	mux.HandleFunc("GET /api/tickets", s.listTickets)
	mux.HandleFunc("POST /api/tickets", s.createTicketAsHuman)
	mux.HandleFunc("GET /api/tickets/{id}", s.getTicket)
	mux.HandleFunc("DELETE /api/tickets/{id}", s.deleteTicket)
	mux.HandleFunc("POST /api/tickets/{id}/comments", s.commentTicketAsHuman)
	mux.HandleFunc("POST /api/tickets/{id}/status", s.setTicketStatusAsHuman)
	mux.HandleFunc("GET /api/events", s.streamEvents)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.updateSettings)
	s.routeMCP(mux)
	s.routeSkills(mux)
	s.routeSchedules(mux)
	s.routeBudgets(mux)
	mux.HandleFunc("GET /api/approvals", s.listApprovals)
	mux.HandleFunc("POST /api/approvals/{id}", s.decideApproval)

	mux.HandleFunc("POST /internal/agents/{address}/hook", s.handleHookEvent)
	mux.HandleFunc("POST /internal/agents/{address}/hook/ack", s.acknowledgeHookDelivery)

	for pattern, h := range d.Extra {
		mux.Handle(pattern, h)
	}

	return GuardCrossSite(d.Origins, mux)
}

func (s *server) lookupEnv(name string) (string, bool) {
	if s.LookupEnv != nil {
		return s.LookupEnv(name)
	}

	return os.LookupEnv(name)
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, Health{Status: "ok", Version: s.Version})
}
