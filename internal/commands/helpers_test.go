package commands

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/investviews/investviews-cli/internal/api"
)

// ⚠️ NOTHING IN THIS PACKAGE'S TESTS TOUCHES THE LIVE API. Every one runs
// against an httptest server, so the suite is deterministic, offline, and
// spends no quota.

type recordedRequest struct {
	Path  string
	Query string
}

// result is one CLI run: what it printed, what it returned, and every HTTP
// request it actually sent — the last of which is how a test proves a locally
// refused request cost nothing.
type result struct {
	stdout   string
	stderr   string
	err      error
	requests []recordedRequest
}

func (r result) hits() int { return len(r.requests) }

func (r result) lastQuery() string {
	if len(r.requests) == 0 {
		return ""
	}
	return r.requests[len(r.requests)-1].Query
}

// run executes one command line against a fake API.
func run(t *testing.T, handler http.HandlerFunc, args ...string) result {
	t.Helper()
	return runWithStdin(t, handler, nil, args...)
}

func runWithStdin(t *testing.T, handler http.HandlerFunc, stdin io.Reader, args ...string) result {
	t.Helper()

	res := result{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res.requests = append(res.requests, recordedRequest{Path: r.URL.Path, Query: r.URL.RawQuery})
		if handler != nil {
			handler(w, r)
		}
	}))
	defer server.Close()

	client, err := api.New(api.Options{BaseURL: server.URL + "/public/v1", Token: "iv_live_ro_test"})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	root := &cobra.Command{Use: "investviews", SilenceUsage: true, SilenceErrors: true}
	for _, sub := range All(Deps{NewClient: func() (*api.Client, error) { return client, nil }}) {
		root.AddCommand(sub)
	}

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetArgs(args)

	res.err = root.Execute()
	res.stdout = stdout.String()
	res.stderr = stderr.String()
	return res
}

// freeHeaders is what every metadata endpoint sends: the literal "uncounted",
// and no X-Quota-Used.
func freeHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Quota-Group", "metadata")
	w.Header().Set("X-Quota-Limit", api.Uncounted)
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

func writeBody(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// freeJSON answers a metadata endpoint.
func freeJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		freeHeaders(w)
		writeBody(w, http.StatusOK, body)
	}
}

// meteredJSON answers a stats endpoint.
func meteredJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		meteredHeaders(w)
		writeBody(w, http.StatusOK, body)
	}
}

func contains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("output does not contain %q\n--- output ---\n%s", needle, haystack)
	}
}

func missing(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("output should not contain %q\n--- output ---\n%s", needle, haystack)
	}
}
