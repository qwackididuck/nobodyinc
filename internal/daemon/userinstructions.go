package daemon

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
)

// readUserInstructions reads the text of the user's own CLAUDE.md, given to
// every agent as part of its system prompt (ADR-0008). It is read at each
// launch, so an edit reaches the agents that start afterwards. A missing file
// means no instructions; another error is logged, and the agent starts
// without them rather than not at all.
//
// Only the text is used: its @imports are not resolved, and the user's
// settings, hooks and MCP servers stay out of the agents (ADR-0003).
func readUserInstructions(logger *slog.Logger, path string) string {
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("cannot read the user's CLAUDE.md: agents start without it", "path", path, "err", err)
		}

		return ""
	}

	return string(data)
}
