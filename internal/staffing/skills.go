package staffing

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// Skills (ADR-0011): the human creates, changes and deletes the project's
// skills (.claude/skills, to be committed) and their own personal skills,
// and copies a personal skill into the project. Nobody Inc writes the
// SKILL.md files itself (internal/skills): Claude Code has no command for
// skills. After each change, the organization is reloaded and every agent
// brought in line, resuming its session (changedAgents).

// SaveSkill creates or changes the SKILL.md of a project or personal skill:
// its description and body; the other keys of its header and the other files
// of its directory are kept.
func (s *Service) SaveSkill(ctx context.Context, scope skills.Scope, name, description, body string) error {
	return s.changeSkill(ctx, scope, name, func(dir string) error {
		if err := skills.Save(dir, description, body); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}

		return nil
	})
}

// DeleteSkill removes a project or personal skill, its whole directory.
func (s *Service) DeleteSkill(ctx context.Context, scope skills.Scope, name string) error {
	return s.changeSkill(ctx, scope, name, func(dir string) error {
		err := skills.Delete(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: no %s skill %q", ErrNotFound, scope, name)
		}

		return err
	})
}

// CopySkillToProject copies a personal skill, its whole directory, into the
// project's .claude/skills. The copy then shadows the personal skill for the
// project's agents.
func (s *Service) CopySkillToProject(ctx context.Context, name string) error {
	from, err := s.skillDir(skills.ScopePersonal, name)
	if err != nil {
		return err
	}

	if _, ok := s.Org.Current().Skills.Get(skills.ScopePersonal, name); !ok {
		return fmt.Errorf("%w: no personal skill %q", ErrNotFound, name)
	}

	return s.changeSkill(ctx, skills.ScopeProject, name, func(to string) error {
		err := skills.Copy(from, to)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: the project already has a skill %q", ErrExists, name)
		}

		return err
	})
}

// changeSkill makes one change in the directory of a skill, then reloads and
// applies.
func (s *Service) changeSkill(ctx context.Context, scope skills.Scope, name string, change func(dir string) error) error {
	dir, err := s.skillDir(scope, name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := change(dir); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{})
}

// skillDir is the directory of a project or personal skill.
func (s *Service) skillDir(scope skills.Scope, name string) (string, error) {
	personal, err := skills.PersonalDir()
	if err != nil {
		return "", err
	}

	dir, err := skills.Dir(s.Org.Current().ProjectDir, personal, scope, name)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	return dir, nil
}
