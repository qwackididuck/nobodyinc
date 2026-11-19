package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
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

// announceKnowledgeRead tells the control room an agent used a knowledge
// tool: the office walks it to the library and back.
func announceKnowledgeRead(bus *events.Bus) func(agent, tool, subject string) {
	return func(agent, tool, subject string) {
		bus.Publish(events.Event{Type: events.KBRead, Data: events.KBReadData{Agent: agent, Tool: tool, Subject: subject}})
	}
}
