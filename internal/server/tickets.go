package server

import (
	"errors"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func (s *server) listTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := s.Tickets.Store.ListTickets(r.Context(), work.Filter{
		Team:     r.URL.Query().Get("team"),
		Status:   work.Status(r.URL.Query().Get("status")),
		Archived: r.URL.Query().Get("archived") == "true",
	})
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, append([]work.Ticket{}, tickets...))
}

// updateTicketBudget changes a ticket's budget; only the human does
// (ADR-0013, point 3).
func (s *server) updateTicketBudget(w http.ResponseWriter, r *http.Request) {
	var b work.Budget
	if err := decodeJSON(w, r, &b, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	t, err := s.Tickets.SetBudget(r.Context(), org.HumanAddress, r.PathValue("id"), b)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, t)
}

// deletedTickets is the body answered by DELETE /api/tickets/{id}.
type deletedTickets struct {
	IDs []string `json:"ids"`
}

// deleteTicket shreds a ticket for good, and with cascade=true the tickets
// that depend on it.
func (s *server) deleteTicket(w http.ResponseWriter, r *http.Request) {
	ids, err := s.Tickets.DeleteTicket(r.Context(), org.HumanAddress, r.PathValue("id"), r.URL.Query().Get("cascade") == "true")
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, deletedTickets{IDs: ids})
}

// ticketDraft is the body of POST /api/tickets.
type ticketDraft struct {
	work.Budget

	Title              string   `json:"title"`
	Team               string   `json:"team"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	BlockedBy          []string `json:"blocked_by"`
	Assignee           string   `json:"assignee"`
}

func (s *server) createTicketAsHuman(w http.ResponseWriter, r *http.Request) {
	var d ticketDraft
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	t, err := s.Tickets.Create(r.Context(), org.HumanAddress, work.Draft{
		Title: d.Title, Team: d.Team, Description: d.Description,
		AcceptanceCriteria: d.AcceptanceCriteria, BlockedBy: d.BlockedBy, Assignee: d.Assignee, Budget: d.Budget,
	})
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, t)
}

// ticketDetails is a ticket with everything attached, including the messages
// exchanged about it.
type ticketDetails struct {
	work.Details

	Messages []messaging.Message `json:"messages"`
}

func (s *server) getTicket(w http.ResponseWriter, r *http.Request) {
	d, err := s.Tickets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.ticketError(w, err)

		return
	}

	messages, err := s.Store.TicketMessages(r.Context(), d.Ticket.ID)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, ticketDetails{Details: d, Messages: append([]messaging.Message{}, messages...)})
}

func (s *server) commentTicketAsHuman(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	c, err := s.Tickets.Comment(r.Context(), org.HumanAddress, r.PathValue("id"), body.Body)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, c)
}

func (s *server) setTicketStatusAsHuman(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status work.Status `json:"status"`
	}
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	t, err := s.Tickets.SetStatus(r.Context(), org.HumanAddress, r.PathValue("id"), body.Status)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, t)
}

// ticketError maps the ticket service's errors to HTTP statuses.
func (s *server) ticketError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, work.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, work.ErrInvalid):
		s.writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, work.ErrForbidden):
		s.writeError(w, http.StatusForbidden, err)
	case errors.Is(err, work.ErrBlocked), errors.Is(err, work.ErrConflict), errors.Is(err, work.ErrNoTicket),
		errors.Is(err, work.ErrUnfinished), isFinishRefusal(err):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.Logger.Error("ticket request", "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("ticket request failed"))
	}
}
