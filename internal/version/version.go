// Package version holds the build stamp, and nothing else.
//
// The three variables below are the only mutable build-time state in the CLI.
// The release pipeline writes them with `go build -ldflags -X`; every other
// build leaves them at their defaults. Two things follow, and both are the
// reason this is its own package:
//
//  1. THE DEFAULTS MUST NEVER BE EMPTY. An unstamped build and a build whose
//     stamping silently failed would otherwise print the same nothing, and
//     "which build is this" is the first question of every support thread.
//     `dev` says "nobody released this"; an empty string says nothing at all.
//
//  2. THE -X PACKAGE PATH IS NOT CHECKED BY ANYTHING AT BUILD TIME. The linker
//     ignores an -X whose package path or variable name does not exist — no
//     error, no warning — so a typo ships a release that reports `dev` forever.
//     version_test.go pins .goreleaser.yaml's -X strings against the module
//     path in go.mod for exactly that reason.
package version

import (
	"fmt"
	"runtime"
)

// Default is what a build with no -ldflags reports: `go build`, `go run`, `go
// test`, and anything a contributor compiles by hand.
const Default = "dev"

var (
	// Version is the released version, without a leading v — goreleaser strips
	// it from the tag, so tag v1.4.0 stamps "1.4.0".
	Version = Default

	// Commit is the full git SHA the release was built from.
	Commit = "none"

	// Date is the build time, RFC 3339, UTC.
	Date = "unknown"
)

// Stamped reports whether this binary carries a release stamp.
//
// ⚠️ It answers about THIS binary, not about whether a release exists. A local
// `go build` is unstamped and that is correct, not a fault.
func Stamped() bool { return Version != Default && Version != "" }

// String is the one line `investviews version` prints. It carries the Go
// toolchain and the platform as well as the stamp, because a bug report that
// omits either is a round trip.
func String() string {
	return fmt.Sprintf("investviews %s (commit %s, built %s, %s, %s/%s)",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Fields is the same answer as a map, for --json. Keys match the flag names an
// agent would grep for.
func Fields() map[string]string {
	return map[string]string{
		"version":  Version,
		"commit":   Commit,
		"date":     Date,
		"go":       runtime.Version(),
		"platform": runtime.GOOS + "/" + runtime.GOARCH,
		"stamped":  fmt.Sprintf("%t", Stamped()),
	}
}
