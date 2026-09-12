package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// expectStampEnv is set by the CI step that links the test binary with the
// release ldflags (see .github/workflows/test.yml). Without it a test cannot
// tell "unstamped because nobody asked" from "the stamp was asked for and did
// not land" — both are just Version == "dev".
const expectStampEnv = "INVESTVIEWS_TEST_EXPECT_STAMP"

// ⚠️ WHAT THIS TEST PROVES, AND WHAT IT DOES NOT.
//
// Proves, in a plain `go test` run: the compiled-in default is non-empty, so a
// binary nobody stamped still answers something instead of printing a blank
// line.
//
// Proves, when the test binary is linked with the release ldflags — which is
// what the "stamped build" CI step does: that the -X actually landed a value.
// `-X <path>.Version=` with nothing after the "=" links an EMPTY string, and
// this is the assertion that catches it.
//
// Does NOT prove that the released ARTEFACT is stamped: `go test` never links
// the released binary. The only check of that is running the built binary —
// `dist/investviews_<os>_<arch>/investviews version` — which is a step of the
// `goreleaser release --snapshot` verification, not of this suite.
func TestVersionIsNeverEmpty(t *testing.T) {
	if Version == "" {
		t.Fatalf("Version is empty; an unstamped build must report %q and a stamped one its tag", Default)
	}
	if Default == "" {
		t.Fatal("Default is empty, so an unstamped build reports nothing")
	}
	if Commit == "" || Date == "" {
		t.Errorf("Commit=%q Date=%q; neither may be empty", Commit, Date)
	}
}

// TestStampLandsWhenTheBuildAsksForIt is the half that can only run under the
// release ldflags. It catches the failure mode the linker is silent about: an
// -X pointing at a package path that does not exist is ignored, the build
// succeeds, and the binary reports "dev".
func TestStampLandsWhenTheBuildAsksForIt(t *testing.T) {
	want := os.Getenv(expectStampEnv)
	if want == "" {
		t.Skipf("unstamped build: set %s and link with the matching -X to run this", expectStampEnv)
	}
	if Version != want {
		t.Fatalf("Version = %q, want %q — the -X did not land; check the package path in the ldflags", Version, want)
	}
	if !Stamped() {
		t.Errorf("Stamped() = false for version %q", Version)
	}
}

// TestGoreleaserStampsThisPackage pins the one string that ties the release
// config to this package: the -X package path.
//
// ⚠️ This is the guard for a rename. Moving or renaming this package, or the
// module, leaves .goreleaser.yaml pointing at a path the linker cannot find —
// which it ignores in silence, so every release after the rename would report
// "dev" and nothing would fail. Reading both files here makes it fail in
// `go test ./...`, before a tag is ever pushed.
//
// ⚠️ The module path is lowercase (github.com/investviews/...) while the GitHub
// repository is Investviews/investviews-cli. Both spellings are deliberate; the
// ldflags must use the module path, which is what this reads out of go.mod.
func TestGoreleaserStampsThisPackage(t *testing.T) {
	module := modulePath(t)
	config := readRepoFile(t, ".goreleaser.yaml")

	for _, name := range []string{"Version", "Commit", "Date"} {
		want := "-X " + module + "/internal/version." + name + "="
		if !strings.Contains(config, want) {
			t.Errorf(".goreleaser.yaml does not stamp %s; expected an ldflag starting %q", name, want)
		}
	}
}

func TestStringAndFieldsCarryTheStamp(t *testing.T) {
	line := String()
	for _, want := range []string{"investviews", Version, Commit, Date} {
		if !strings.Contains(line, want) {
			t.Errorf("String() = %q, missing %q", line, want)
		}
	}
	fields := Fields()
	if fields["version"] != Version {
		t.Errorf(`Fields()["version"] = %q, want %q`, fields["version"], Version)
	}
	if _, ok := fields["platform"]; !ok {
		t.Error("Fields() has no platform")
	}
}

// repoRoot is two directories up from this package. The test binary runs in
// the package directory, so this is stable without any build tag trickery.
func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatalf("cannot read %s from the repository root: %v", name, err)
	}
	return string(body)
}

func modulePath(t *testing.T) string {
	t.Helper()
	for _, line := range strings.Split(readRepoFile(t, "go.mod"), "\n") {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatal("go.mod has no module line")
	return ""
}
