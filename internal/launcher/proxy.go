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
// terminals) and flushes event streams as they come (/api/events). The
// browser's Origin header goes through untouched: the project daemon checks
// it against the same allowed origins as the launcher.
func newProxy(target *url.URL, prefix string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
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
