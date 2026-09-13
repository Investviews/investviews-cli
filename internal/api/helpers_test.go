package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// recorder is a test client plus the sleeps the retry loop asked for. Nothing
// ever waits: Sleep records the duration and returns.
type recorder struct {
	server *httptest.Server
	client *Client
	sleeps []time.Duration
	// requests is one entry per HTTP attempt, path and query included.
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
}

func (r *recorder) hits() int { return len(r.requests) }

func (r *recorder) last() recordedRequest {
	if len(r.requests) == 0 {
		return recordedRequest{}
	}
	return r.requests[len(r.requests)-1]
}

// newClient starts an httptest server running handler and returns a client
// pointed at it. Jitter is the identity so backoff is exact, and Sleep never
// waits, so a "retry timing" test runs in microseconds.
func newClient(t *testing.T, handler http.HandlerFunc, tweak ...func(*Options)) *recorder {
	t.Helper()

	rec := &recorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.requests = append(rec.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Header: r.Header.Clone(),
		})
		handler(w, r)
	}))
	t.Cleanup(rec.server.Close)

	opts := Options{
		BaseURL: rec.server.URL + "/public/v1",
		Token:   "iv_live_ro_testtoken",
		Jitter:  func(d time.Duration) time.Duration { return d },
		Sleep: func(ctx context.Context, d time.Duration) error {
			rec.sleeps = append(rec.sleeps, d)
			return ctx.Err()
		},
	}
	for _, f := range tweak {
		f(&opts)
	}

	client, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec.client = client
	return rec
}

// writeJSON answers with a JSON body and the quota headers a free call carries.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// freeHeaders is what a metadata endpoint sends: the literal "uncounted", and
// no X-Quota-Used.
func freeHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Quota-Group", "metadata")
	w.Header().Set("X-Quota-Limit", Uncounted)
	w.Header().Set("X-RateLimit-Limit", "120")
	w.Header().Set("X-RateLimit-Remaining", "119")
	w.Header().Set("X-Request-Id", "req-free")
}

// meteredHeaders is what /stats/current sends.
func meteredHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Quota-Group", "current")
	w.Header().Set("X-Quota-Limit", "100000")
	w.Header().Set("X-Quota-Used", "138")
	w.Header().Set("X-Quota-Reset", "2026-10-11T12:30:25Z")
	w.Header().Set("X-RateLimit-Limit", "300")
	w.Header().Set("X-RateLimit-Remaining", "299")
	w.Header().Set("X-Request-Id", "req-metered")
}

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }
