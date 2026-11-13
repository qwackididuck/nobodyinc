package server

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Errors of the cross-site guard.
var (
	errCrossOrigin = errors.New("cross-origin request refused: only the control room may change things")
	errNotJSON     = errors.New("request body must be application/json")
)

// guardCrossSite protects the API routes that change something (anything but
// GET and HEAD) from requests a web page could send on the user's behalf:
//
//   - a request carrying an Origin header (browsers always send one on
//     cross-origin POST, PUT and DELETE) must come from the daemon itself or
//     from an allowed origin (the control room, Deps.Origins);
//   - a body must be declared application/json, which a page cannot send to
//     another origin without the browser asking the daemon first (CORS
//     preflight), and the daemon never allows it.
//
// Requests without an Origin header (nobody-hook, curl, the control room's
// server-side proxy) are not browser requests and go through. This is not
// authentication: anything running as the user can still call the API (see
// docs/architecture.md, VPS mode).
func guardCrossSite(allowedOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)

			return
		}

		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host, allowedOrigins) {
			writeGuardError(w, http.StatusForbidden, errCrossOrigin)

			return
		}

		if hasBody(r) && !isJSON(r.Header.Get("Content-Type")) {
			writeGuardError(w, http.StatusUnsupportedMediaType, errNotJSON)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin is the daemon itself (same host) or
// matches one of the allowed host patterns, e.g. "localhost:3000" (the same
// patterns as the terminal WebSocket).
func originAllowed(origin, host string, allowed []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}

	if strings.EqualFold(u.Host, host) {
		return true
	}

	for _, pattern := range allowed {
		if ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(u.Host)); ok {
			return true
		}
	}

	return false
}

func hasBody(r *http.Request) bool {
	return r.ContentLength > 0 || len(r.TransferEncoding) > 0
}

func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)

	return err == nil && mediaType == "application/json"
}

// writeGuardError answers like writeError, without needing the server.
func writeGuardError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`)) // the client may be gone; nothing to do
}
