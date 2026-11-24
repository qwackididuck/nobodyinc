package staffing

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// A change of the project's environment restarts every agent, resuming its
// session and once idle: no fresh session, no interruption (ADR-0020).
func TestApplyEveryone(t *testing.T) {
	t.Parallel()

	s, a := newService(t)

	if err := s.ApplyEveryone(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(a.calls, "dev1@dev") {
		t.Fatalf("applied %v: want dev1@dev resumed once idle (no fresh, no now)", a.calls)
	}

	for _, call := range a.calls {
		if strings.Contains(call, " ") {
			t.Fatalf("applied %q: want a plain resume, neither fresh nor at once", call)
		}
	}
}
