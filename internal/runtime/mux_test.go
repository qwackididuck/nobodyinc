package runtime

import (
	"log/slog"
	"net/http"
)

func newTerminalMux(s *Supervisor) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /terminal/{address}", TerminalHandler(s, nil, slog.New(slog.DiscardHandler)))

	return mux
}
