package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/investviews/investviews-cli/internal/version"
)

// runRoot executes one command line against the real command tree and returns
// what it printed. Nothing here reaches the network: "version" is answered
// entirely from the build stamp.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// ⚠️ WHAT THIS PROVES: the command prints a NON-EMPTY version, whatever the
// build. Under `go test` that is the "dev" default; under the CI step that
// links with the release ldflags it is the stamped tag, so the same assertion
// covers a release build.
//
// ⚠️ WHAT IT DOES NOT PROVE: that the published artefact is stamped. This test
// runs against a test binary, never against the binary goreleaser ships. That
// one is checked by running it — see the snapshot verification in
// .goreleaser.yaml's header.
func TestVersionCommandPrintsANonEmptyVersion(t *testing.T) {
	// No token, no config file: a support answer must not depend on either.
	t.Setenv("INVESTVIEWS_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	out, err := runRoot(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	line := strings.TrimSpace(out)
	if line == "" {
		t.Fatal("version printed nothing")
	}
	if !strings.Contains(line, version.Version) || version.Version == "" {
		t.Errorf("version printed %q, which does not carry the stamp %q", line, version.Version)
	}
	for _, want := range []string{"investviews", version.Commit, version.Date} {
		if !strings.Contains(line, want) {
			t.Errorf("version printed %q, missing %q", line, want)
		}
	}
}

func TestVersionCommandJSONCarriesTheSameStamp(t *testing.T) {
	out, err := runRoot(t, "version", "--json")
	if err != nil {
		t.Fatalf("version --json: %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		t.Fatalf("version --json did not print JSON: %v\n%s", err, out)
	}
	if fields["version"] != version.Version || fields["version"] == "" {
		t.Errorf(`version --json gave version=%q, want %q`, fields["version"], version.Version)
	}
	if fields["platform"] == "" {
		t.Error("version --json gave no platform")
	}
}

// The root --version flag and the subcommand read the same variable; a release
// where they disagreed would be a support trap.
func TestRootVersionFlagMatchesTheSubcommand(t *testing.T) {
	flagOut, err := runRoot(t, "--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(flagOut, version.Version) {
		t.Errorf("--version printed %q, want it to carry %q", strings.TrimSpace(flagOut), version.Version)
	}
}
