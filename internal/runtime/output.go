package runtime

import "sync"

// backlogSize is how much recent terminal output is kept per agent, so that a
// viewer opening the terminal late still sees the current screen.
const backlogSize = 256 << 10

// viewerBuffer is how many output chunks may wait for a slow viewer before it
// is disconnected (it can reconnect and get the backlog again).
const viewerBuffer = 512

// Output keeps an agent's recent terminal output and fans it out to viewers.
// It implements io.Writer: the PTY reader writes into it.
type Output struct {
	mu      sync.Mutex
	backlog []byte
	viewers map[chan []byte]struct{}
}

// NewOutput returns an empty Output.
func NewOutput() *Output {
	return &Output{viewers: map[chan []byte]struct{}{}}
}

// Write records a chunk of terminal output and sends it to every viewer.
func (o *Output) Write(p []byte) (int, error) {
	chunk := append([]byte(nil), p...)

	o.mu.Lock()
	defer o.mu.Unlock()

	o.backlog = append(o.backlog, chunk...)
	if extra := len(o.backlog) - backlogSize; extra > 0 {
		o.backlog = append([]byte(nil), o.backlog[extra:]...)
	}

	for ch := range o.viewers {
		select {
		case ch <- chunk:
		default:
			// Too slow: dropping a chunk would corrupt the screen, so the viewer
			// is disconnected instead.
			delete(o.viewers, ch)
			close(ch)
		}
	}

	return len(p), nil
}

// Subscribe returns, in this order: the current backlog, a channel receiving
// the output that follows, and a cancel function. The channel is closed when
// the viewer is too slow or calls cancel.
func (o *Output) Subscribe() ([]byte, <-chan []byte, func()) {
	ch := make(chan []byte, viewerBuffer)

	o.mu.Lock()
	backlog := append([]byte(nil), o.backlog...)
	o.viewers[ch] = struct{}{}
	o.mu.Unlock()

	cancel := func() {
		o.mu.Lock()
		defer o.mu.Unlock()

		if _, ok := o.viewers[ch]; ok {
			delete(o.viewers, ch)
			close(ch)
		}
	}

	return backlog, ch, cancel
}
