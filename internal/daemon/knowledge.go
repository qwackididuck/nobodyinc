package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// The knowledge base (ADR-0019): a folder of the project, declared in
// org.yaml, read by kb.Service for the API and the agents' knowledge tools.
// It is scanned when read, and every few seconds in the background, so that
// the control room hears of pages changed by agents or by hand
// (events.KBChanged).

// defaultKnowledgeScan is how often the knowledge base is scanned in the
// background (Options.KnowledgeScanEvery).
const defaultKnowledgeScan = 5 * time.Second

// knowledgeScans is the knowledge base service and its background scans,
// which stop is called to end; done is closed once they have (before the
// database closes).
type knowledgeScans struct {
	*kb.Service

	stop context.CancelFunc
	done chan struct{}
}

// startKnowledge builds the project's knowledge base service and starts
// its background scans (startKnowledgeScans).
func startKnowledge(ctx context.Context, opts Options, st *store.Store, bus *events.Bus) knowledgeScans {
	k := knowledgeScans{Service: newKnowledge(opts, st, bus)}
	k.stop, k.done = startKnowledgeScans(ctx, opts, k.Service)

	return k
}

// newKnowledge builds the project's knowledge base service. The folder is
// the one org.yaml declares at each scan: the human may set it up, move it
// or remove it at any time.
func newKnowledge(opts Options, st *store.Store, bus *events.Bus) *kb.Service {
	return &kb.Service{
		ProjectDir: opts.Org.Current().ProjectDir,
		Declared:   func() string { return opts.Org.Current().KnowledgePath() },
		Index:      st,
		Logger:     opts.Logger,
		OnChange: func(u kb.Update) {
			bus.Publish(events.Event{Type: events.KBChanged, Data: events.KBChangedData{
				Pages: u.Pages, Added: u.Added, Removed: u.Removed, Changed: u.Changed, Reset: u.Reset,
			}})
		},
	}
}

// startKnowledgeScans scans the knowledge base at once, then every
// opts.KnowledgeScanEvery, until ctx ends or the returned function is
// called; the channel is closed once it has stopped (before the database
// closes).
func startKnowledgeScans(ctx context.Context, opts Options, knowledge *kb.Service) (context.CancelFunc, chan struct{}) {
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	context.AfterFunc(ctx, stop)

	every := opts.KnowledgeScanEvery
	if every <= 0 {
		every = defaultKnowledgeScan
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		if _, err := knowledge.Refresh(runCtx); err != nil && !errors.Is(err, kb.ErrNone) && runCtx.Err() == nil {
			opts.Logger.Warn("scan the knowledge base", "err", err)
		}

		knowledge.Run(runCtx, every)
	}()

	return stop, done
}

// watchKnowledgeFiles follows what agents do with the knowledge base's
// files through their own tools (the hooks, server.Deps.ToolUsed): agents
// read and write the documentation with Read and Edit as often as with the
// knowledge tools.
//   - PreToolUse of a file tool on a file of the knowledge folder (in the
//     agent's working directory, its worktree during a feature, or in the
//     main checkout): the control room hears it like a knowledge tool
//     (events.KBRead), and the office walks the agent to the library;
//   - PostToolUse of a tool that changed such a file: the knowledge base is
//     scanned at once, so that the library shows the change without waiting
//     for the next background scan (in the main checkout only: a worktree's
//     changes reach it with "Finish").
func watchKnowledgeFiles(
	ctx context.Context, opts Options, workDir func(string) string, knowledge *kb.Service, bus *events.Bus,
) func(string, hook.Event) {
	ctx = context.WithoutCancel(ctx)

	return func(address string, ev hook.Event) {
		o := opts.Org.Current()

		declared := o.KnowledgePath()
		if declared == "" {
			return
		}

		rel, ok := knowledgeFile(declared, ev, workDir(address), o.ProjectDir)
		if !ok {
			return
		}

		switch ev.Name {
		case hook.EventPreToolUse:
			bus.Publish(events.Event{Type: events.KBRead, Data: events.KBReadData{Agent: address, Tool: ev.ToolName, Subject: rel}})
		case hook.EventPostToolUse:
			if slices.Contains(hook.WritingTools, ev.ToolName) {
				// The hook must not wait for the scan.
				go func() {
					if _, err := knowledge.Refresh(ctx); err != nil && !errors.Is(err, kb.ErrNone) {
						opts.Logger.Debug("scan the knowledge base after an edit", "err", err)
					}
				}()
			}
		}
	}
}

// knowledgeFile tells whether the file a hook event's tool names is in the
// knowledge folder declared under one of roots (the agent's working
// directory, the main checkout), and returns its path in that folder. A
// relative path is the tool's, from the event's working directory. Hidden
// files are not part of the knowledge base.
func knowledgeFile(declared string, ev hook.Event, roots ...string) (string, bool) {
	p := ev.FilePath()
	if p == "" {
		return "", false
	}

	if !filepath.IsAbs(p) {
		p = filepath.Join(ev.Cwd, p)
	}

	p = filepath.Clean(p)

	for _, root := range rootsWithLinks(roots) {
		rel, err := filepath.Rel(filepath.Join(root, filepath.FromSlash(declared)), p)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		rel = filepath.ToSlash(rel)
		if slices.ContainsFunc(strings.Split(rel, "/"), func(seg string) bool { return strings.HasPrefix(seg, ".") }) {
			return "", false
		}

		return rel, true
	}

	return "", false
}

// rootsWithLinks is roots, each also with its symbolic links followed:
// Claude Code may name a file through either (on macOS, /var is a link to
// /private/var).
func rootsWithLinks(roots []string) []string {
	var all []string

	for _, root := range roots {
		if root == "" {
			continue
		}

		all = append(all, root)
		if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
			all = append(all, resolved)
		}
	}

	return all
}

// announceKnowledgeRead tells the control room an agent used a knowledge
// tool: the office walks it to the library and back.
func announceKnowledgeRead(bus *events.Bus) func(agent, tool, subject string) {
	return func(agent, tool, subject string) {
		bus.Publish(events.Event{Type: events.KBRead, Data: events.KBReadData{Agent: agent, Tool: tool, Subject: subject}})
	}
}
