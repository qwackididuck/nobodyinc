package store

import (
	"slices"
	"testing"
)

func TestAgentSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		do   func(t *testing.T, s *Store)
		want []AgentSession
	}{
		{
			name: "a saved session starts out of a turn",
			do: func(t *testing.T, s *Store) {
				t.Helper()
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-1", "/p"))
			},
			want: []AgentSession{{Address: "alice@dev", SessionID: "s-1", WorkDir: "/p"}},
		},
		{
			name: "the same session keeps its mid-turn flag",
			do: func(t *testing.T, s *Store) {
				t.Helper()
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-1", "/p"))
				mustNot(t, s.SetAgentMidTurn(t.Context(), "alice@dev", true))
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-1", "/p"))
			},
			want: []AgentSession{{Address: "alice@dev", SessionID: "s-1", WorkDir: "/p", MidTurn: true}},
		},
		{
			name: "a new session replaces the old one, out of a turn",
			do: func(t *testing.T, s *Store) {
				t.Helper()
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-1", "/p"))
				mustNot(t, s.SetAgentMidTurn(t.Context(), "alice@dev", true))
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-2", "/w"))
			},
			want: []AgentSession{{Address: "alice@dev", SessionID: "s-2", WorkDir: "/w"}},
		},
		{
			name: "mid-turn without a session records nothing",
			do: func(t *testing.T, s *Store) {
				t.Helper()
				mustNot(t, s.SetAgentMidTurn(t.Context(), "bob@dev", true))
			},
		},
		{
			name: "a forgotten session is gone",
			do: func(t *testing.T, s *Store) {
				t.Helper()
				mustNot(t, s.SaveAgentSession(t.Context(), "alice@dev", "s-1", "/p"))
				mustNot(t, s.SaveAgentSession(t.Context(), "bob@dev", "s-9", "/p"))
				mustNot(t, s.ForgetAgentSession(t.Context(), "alice@dev"))
			},
			want: []AgentSession{{Address: "bob@dev", SessionID: "s-9", WorkDir: "/p"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := openTestStore(t)
			tt.do(t, s)

			got, err := s.AgentSessions(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("sessions = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func mustNot(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
