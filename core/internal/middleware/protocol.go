package middleware

import (
	"net/http"
	"strings"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// RequireProtocol prevents stale clients from interpreting task and run IDs
// with the pre-unification meanings. Public callbacks and health checks do not
// use this middleware.
func RequireProtocol(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(protocol.GenerationHeader, protocol.Generation)
		download := (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/api/attachments/") && strings.HasSuffix(r.URL.Path, "/download")
		if !download && r.Header.Get(protocol.GenerationHeader) != protocol.Generation {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUpgradeRequired)
			_, _ = w.Write([]byte(`{"error":"unsupported protocol generation","required_protocol":"task-run-v1"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
