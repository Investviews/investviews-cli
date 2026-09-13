package api

import (
	"fmt"
	"strconv"
	"strings"
)

// ⚠️ A CELL ID IS CHECKED BEFORE IT IS SPENT.
//
// h3= takes its values from three places — a repeated flag, a comma list, and
// standard input — and the third is the dangerous one. A piped listing is
// RENDERED TEXT: a header line, an availability sentence, a "more cells" hint
// and a cost line all tokenise into words that sit in the list looking exactly
// like ids. Unchecked, they go out as h3= on a METERED endpoint, so the
// mistake is paid for before anything notices it.
//
// Checking here costs nothing and no request is sent.

// An H3 cell index occupies one band of the 64-bit space. Bit 63 is a reserved
// zero and bits 59..62 are the mode, which is 1 for a cell — so v>>59 == 1
// holds for every cell at every resolution, and for nothing else.
const (
	h3CellModeShift = 59
	h3CellMode      = 1
)

// IsH3Cell reports whether an index names a cell rather than some other H3
// object or an arbitrary number.
func IsH3Cell(index uint64) bool { return index>>h3CellModeShift == h3CellMode }

// ParseH3Cell reads one cell id in the two forms the contract documents: a
// DECIMAL uint64 string (613498076398616575), which is what every endpoint
// here emits, and a canonical H3 string (8839540ad1fffff). Both name the same
// cell and both are accepted verbatim by /stats/*, so both are accepted here.
//
// ⚠️ THIS IS A CELL CHECK, NOT A DIGIT CHECK. "3" and "500" are perfectly good
// decimal numbers and are not cells — and they are precisely what a rendered
// listing leaks into a pipe ("3 cell(s)", "at res 8", "500 cell(s)"). A
// digits-only test would pass them straight through to the metered call.
func ParseH3Cell(token string) (uint64, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, fmt.Errorf("an H3 cell id cannot be empty")
	}
	// Decimal first: it is the form this API emits, so it is the form a
	// pipe carries. A canonical string is only reachable when a human
	// typed one.
	if index, err := strconv.ParseUint(trimmed, 10, 64); err == nil && IsH3Cell(index) {
		return index, nil
	}
	if index, err := strconv.ParseUint(trimmed, 16, 64); err == nil && IsH3Cell(index) {
		return index, nil
	}
	return 0, fmt.Errorf("%q is not an H3 cell id", trimmed)
}

// ValidateH3Cells refuses a cell list before any of it is spent, naming the
// token that is wrong.
//
// ⚠️ It names the OFFENDING TOKEN, not just the count. A piped listing fails
// on its first word, and "R14727511 is not a cell id" says which pipe produced
// it; "one of the 8 cells is invalid" does not.
func ValidateH3Cells(cells []string) error {
	for _, cell := range cells {
		if _, err := ParseH3Cell(cell); err != nil {
			return &ValidationError{
				Parameter: "h3",
				Message: fmt.Sprintf("%v. A cell id is a decimal uint64 (613498076398616575) or a "+
					"canonical H3 string (8839540ad1fffff), and both come back verbatim from "+
					"`investviews geo hexes`. Nothing was sent, so this cost no quota. "+
					"If you piped a listing in, pipe the ids ALONE — "+
					"`investviews geo hexes <geo_id> --all --ids-only | investviews stats current --h3 -` "+
					"— because the default listing also prints a header, an availability line and a "+
					"cost line, and those are not cell ids", err),
			}
		}
	}
	return nil
}
