// Package server exposes the nobodyd HTTP API described in api/openapi.yaml.
//
// Routes are grouped by audience:
//
//   - api.go:       /api/*           for the supervision UI and the human operator;
//   - approvals.go: /api/approvals*  permission requests waiting for the human;
//   - tickets.go:   /api/tickets*    tickets, as seen and handled by the human;
//   - features.go:  /api/features    features and their git branches;
//   - hooks.go:     /internal/*      for nobody-hook, called by Claude Code hooks.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Deps are the services the HTTP layer relies on.
type Deps struct {
	Version  string
	Logger   *slog.Logger
	Org      *org.Org
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
	// Extra mounts additional handlers (MCP endpoint, terminals) by pattern.
	Extra map[string]http.Handler
}

// AgentStarter launches an agent's session (implemented by runtime.Supervisor).
type AgentStarter interface {
	Start(ctx context.Context, address string) error
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
	mux.HandleFunc("GET /api/messages", s.listMessages)
	mux.HandleFunc("POST /api/messages", s.sendMessageAsHuman)
	mux.HandleFunc("GET /api/features", s.listFeatures)
	mux.HandleFunc("POST /api/features", s.startFeature)
	mux.HandleFunc("GET /api/tickets", s.listTickets)
	mux.HandleFunc("POST /api/tickets", s.createTicketAsHuman)
	mux.HandleFunc("GET /api/tickets/{id}", s.getTicket)
	mux.HandleFunc("POST /api/tickets/{id}/comments", s.commentTicketAsHuman)
	mux.HandleFunc("POST /api/tickets/{id}/status", s.setTicketStatusAsHuman)
	mux.HandleFunc("GET /api/approvals", s.listApprovals)
	mux.HandleFunc("POST /api/approvals/{id}", s.decideApproval)

	mux.HandleFunc("POST /internal/agents/{address}/hook", s.handleHookEvent)
	mux.HandleFunc("POST /internal/agents/{address}/hook/ack", s.acknowledgeHookDelivery)

	for pattern, h := range d.Extra {
		mux.Handle(pattern, h)
	}

	return mux
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, Health{Status: "ok", Version: s.Version})
}
