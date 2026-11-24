package staffing

import (
	"context"
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// ApplyEveryone brings every agent in line with a change that is not in the
// organization's files, such as the project's environment variables
// (ADR-0020): each one restarts once idle, resuming its session, and so gets
// the new environment. Nothing interrupts a turn (runtime.Supervisor.Apply).
func (s *Service) ApplyEveryone(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	for _, m := range s.Org.Current().Members() {
		if err := s.Agents.Apply(ctx, m.Address, runtime.ApplyOptions{}); err != nil {
			errs = append(errs, fmt.Errorf("restart %s: %w", m.Address, err))
		}
	}

	return errors.Join(errs...)
}
