package httpx

import "net/http"

// JSONFallback serves mux, but answers unmatched routes with the JSON error shape
// (404 not_found, or 405 method_not_allowed plus the Allow header) instead of ServeMux's
// plain-text bodies.
func JSONFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallback, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		probe := &probeWriter{header: http.Header{}}
		fallback.ServeHTTP(probe, r)
		if probe.status != http.StatusMethodNotAllowed {
			writeAPIError(w, errRouteNotFound)
			return
		}
		w.Header().Set("Allow", probe.header.Get("Allow"))
		writeAPIError(w, errMethodNotAllowed)
	})
}

// probeWriter records what ServeMux's default handlers would have sent and drops the body.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header         { return p.header }
func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *probeWriter) WriteHeader(code int)        { p.status = code }
