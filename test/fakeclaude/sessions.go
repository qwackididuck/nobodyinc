package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Sessions, as Claude Code keeps them (ADR-0015): every session is a file
// in $CLAUDE_CONFIG_DIR/fake-sessions, with the directory it started in and
// what it was told (its memory, for the "MEMORY?" query). --resume <id>
// continues a session of the current directory; like the real CLI, resuming
// one it does not have prints "No conversation found with session ID: <id>"
// and exits with status 1, before any hook.

// errNoConversation is resuming a session that does not exist here.
var errNoConversation = errors.New("no conversation found")

// session is one fake Claude Code session.
type session struct {
	ID     string   `json:"id"`
	Dir    string   `json:"dir"`
	Memory []string `json:"memory"`

	path string
}

// sessionsDir is where the fake sessions live.
func sessionsDir() string {
	return filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "fake-sessions")
}

// openSession starts a new session in the current directory, or continues
// the one --resume names.
func openSession(resume string) (*session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if resume == "" {
		id := "fake-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())
		s := &session{ID: id, Dir: cwd, Memory: []string{}, path: filepath.Join(sessionsDir(), id+".json")}

		if err := s.save(); err != nil {
			return nil, err
		}

		return s, nil
	}

	path := filepath.Join(sessionsDir(), filepath.Base(resume)+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w with session ID: %s", errNoConversation, resume)
	}

	var s session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	// Claude Code keeps sessions per directory.
	if s.Dir != cwd {
		return nil, fmt.Errorf("%w with session ID: %s", errNoConversation, resume)
	}

	s.path = path

	return &s, nil
}

// remember adds a line to the session's memory.
func (s *session) remember(line string) {
	s.Memory = append(s.Memory, line)

	if err := s.save(); err != nil {
		sayf("save the session: %v", err)
	}
}

// save writes the session to its file, in sessionsDir (the session ID is
// reduced to a base name when resuming).
func (s *session) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil { //nolint:gosec // our own sessions directory
		return err
	}

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, workFilePerm) //nolint:gosec // a file of our own sessions directory
}
