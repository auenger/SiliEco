package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func TestRequireProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, generation string
		want             int
	}{
		{"missing", "", http.StatusUpgradeRequired},
		{"old", "issue-task-v1", http.StatusUpgradeRequired},
		{"current", protocol.Generation, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := RequireProtocol(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
			req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
			req.Header.Set(protocol.GenerationHeader, tc.generation)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want || called != (tc.want == http.StatusNoContent) {
				t.Fatalf("status=%d called=%v", res.Code, called)
			}
			if got := res.Header().Get(protocol.GenerationHeader); got != protocol.Generation {
				t.Fatalf("generation=%q", got)
			}
		})
	}
}
