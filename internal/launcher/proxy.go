package launcher

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// newProxy forwards the control room's requests for a project,
// <prefix>/api/..., to its daemon's /api/....
//
// httputil.ReverseProxy also carries WebSocket upgrades (the agents'
// terminals) and flushes event streams as they come (/api/events).
//
// The browser's Origin and the request's Host both go through untouched: the
// project daemon checks the Origin against its Host (server.GuardCrossSite,
// and the terminals' WebSocket), and against the same allowed origins as the
// launcher. Keeping the Host matters when the launcher serves the control
// room itself (ADR-0016): the page's origin is the launcher, which SetURL
// would otherwise replace by the daemon's own address, and every change
// would be refused as cross-origin.
func newProxy(target *url.URL, prefix string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, prefix)
			r.Out.URL.RawPath = strings.TrimPrefix(r.In.URL.RawPath, prefix)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadGateway, err)
		},
	}
}

// proxyToProject serves /p/{project}/...: the project's daemon answers, or
// 503 while the project is not running (no window holds it, or it is still
// starting).
func (l *Launcher) proxyToProject(w http.ResponseWriter, r *http.Request) {
	proc := l.runningProcess(r.PathValue("project"))
	if proc == nil {
		writeError(w, http.StatusServiceUnavailable, errNotRunning)

		return
	}

	proc.proxy.ServeHTTP(w, r)
}
