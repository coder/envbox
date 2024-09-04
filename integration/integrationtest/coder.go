package integrationtest

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
)

type BuildLogRecorder struct {
	mu   sync.Mutex
	logs []string
}

func (b *BuildLogRecorder) ContainsLog(l string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, log := range b.logs {
		if log == l {
			return true
		}
	}
	return false
}

func (b *BuildLogRecorder) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.logs)
}

func (b *BuildLogRecorder) append(log string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.logs = append(b.logs, log)
}

// FakeBuildLogRecorder starts a server that fakes a Coder
// deployment for the purpose of pushing build logs.
// It returns a type for asserting that expected log
// make it through the expected endpoint.
func FakeBuildLogRecorder(t testing.TB, l net.Listener, cert tls.Certificate) *BuildLogRecorder {
	t.Helper()

	recorder := &BuildLogRecorder{}
	mux := http.NewServeMux()
	mux.Handle("/api/v2/buildinfo", http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(true)

			// We can't really do much about these errors, it's probably due to a
			// dropped connection.
			_ = enc.Encode(&codersdk.BuildInfoResponse{
				Version: "v1.0.0",
			})
		}))

	mux.Handle("/api/v2/workspaceagents/me/logs", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var logs agentsdk.PatchLogs
			err := json.NewDecoder(r.Body).Decode(&logs)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			for _, log := range logs.Logs {
				recorder.append(log.Output)
			}
		}))

	mux.Handle("/", http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			t.Fatalf("unexpected route %v", r.URL.Path)
		}))

	s := httptest.NewUnstartedServer(mux)
	s.Listener = l
	if cert.Certificate != nil {
		//nolint:gosec
		s.TLS = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		s.StartTLS()
	} else {
		s.Start()
	}

	t.Cleanup(s.Close)
	return recorder
}
