package staffing

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Commands (ADR-0017): the human creates, changes and deletes the project's
// commands (.claude/commands, to be committed) and their own personal
// commands, and copies a personal command into the project. Nobody Inc
// writes the command files itself (internal/commands). After each change,
// the organization is reloaded and every agent brought in line, resuming its
// session (changedAgents).

// SaveCommand creates or changes a project or personal command: its
// description and prompt; the other keys of its header are kept. An existing
// command is written where it is found (its file may be a link); a new one at
// the path its name gives (commands.Path).
func (s *Service) SaveCommand(ctx context.Context, scope commands.Scope, name, description, body string) error {
	path, err := s.commandPath(scope, name)
	if err != nil {
		return err
	}

	return s.changeCommands(ctx, func() error {
		if err := commands.Save(path, description, body); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		return nil
	})
}

// DeleteCommand removes a project or personal command, and the directories
// it leaves empty.
func (s *Service) DeleteCommand(ctx context.Context, scope commands.Scope, name string) error {
	c, ok := s.Org.Current().Commands.Get(scope, name)
	if !ok {
		return fmt.Errorf("%w: no %s command %q", ErrNotFound, scope, name)
	}

	root, err := s.commandsRoot(scope)
	if err != nil {
		return err
	}

	return s.changeCommands(ctx, func() error {
		err := commands.Delete(root, c.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: no %s command %q", ErrNotFound, scope, name)
		}

		return err
	})
}

// CopyCommandToProject copies a personal command into the project's
// .claude/commands, at the same path. The copy then shadows the personal
// command for the project's agents.
func (s *Service) CopyCommandToProject(ctx context.Context, name string) error {
	c, ok := s.Org.Current().Commands.Get(commands.ScopePersonal, name)
	if !ok {
		return fmt.Errorf("%w: no personal command %q", ErrNotFound, name)
	}

	to, err := s.newCommandPath(commands.ScopeProject, name)
	if err != nil {
		return err
	}

	return s.changeCommands(ctx, func() error {
		err := commands.Copy(c.Path, to)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: the project already has a command %q", ErrExists, name)
		}

		return err
	})
}

// changeCommands makes one change to the command files, then reloads and
// applies.
func (s *Service) changeCommands(ctx context.Context, change func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := change(); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{})
}

// commandPath is the file of a project or personal command: where it is when
// it exists, else where its name puts it.
func (s *Service) commandPath(scope commands.Scope, name string) (string, error) {
	if !scope.Valid() {
		return "", fmt.Errorf("%w: command scope %q: use project or personal", ErrInvalid, scope)
	}

	if c, ok := s.Org.Current().Commands.Get(scope, name); ok {
		return c.Path, nil
	}

	return s.newCommandPath(scope, name)
}

// newCommandPath is where a new command of that scope and name goes.
func (s *Service) newCommandPath(scope commands.Scope, name string) (string, error) {
	personal, err := commands.PersonalDir()
	if err != nil {
		return "", err
	}

	path, err := commands.Path(s.Org.Current().ProjectDir, personal, scope, name)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	return path, nil
}

// commandsRoot is the commands directory of a scope.
func (s *Service) commandsRoot(scope commands.Scope) (string, error) {
	switch scope {
	case commands.ScopeProject:
		return commands.ProjectDir(s.Org.Current().ProjectDir), nil
	case commands.ScopePersonal:
		return commands.PersonalDir()
	default:
		return "", fmt.Errorf("%w: command scope %q: use project or personal", ErrInvalid, scope)
	}
}
