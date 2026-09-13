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

// The 64 bits of an H3 index, highest first. Every field below is checked
// except the one noted:
//
//	bit  63      reserved, always 0
//	bits 59..62  mode; 1 means "cell"
//	bits 56..58  mode-dependent bits; 0 for a cell
//	bits 52..55  resolution, 0..15
//	bits 45..51  base cell, 0..121
//	bits 0..44   15 digits of 3 bits each, digit r at shift (15-r)*3
//
// Digits 1..res are the path down from the base cell and hold 0..6; digits
// above res are unused and hold 7. Both halves of that are checked: a used
// digit of 7 and an unused digit that is not 7 are each a rejection.
//
// ⚠️ Resolution is not range-checked because it cannot fail. The field is four
// bits wide, so 0..15 is every value it can hold — a check there would be a
// branch no input can take, which is worse than no check at all.
//
// ⚠️ WHAT THIS DOES NOT CHECK: the pentagon deleted subsequence. Twelve of the
// 122 base cells are pentagons, and on those the first non-zero digit may not
// be 1 — that path does not exist on the grid. Ruling it out needs the base
// cell pentagon table, which is the point where this stops being a few bit
// tests and starts being an H3 implementation. So a structurally well-formed
// id naming a pentagon path that does not exist is ACCEPTED here and refused
// by the server. That residue is the only false accept left; it costs one 400
// on a metered endpoint, which is the trade taken deliberately — a check that
// wrongly REFUSED a real cell would break the whole --h3 surface, and a false
// accept costs one request.
const (
	h3HighBitShift  = 63
	h3ModeShift     = 59
	h3ModeMask      = 0xf
	h3CellMode      = 1
	h3ReservedShift = 56
	h3ReservedMask  = 0x7
	h3ResShift      = 52
	h3ResMask       = 0xf
	h3BaseCellShift = 45
	h3BaseCellMask  = 0x7f
	h3NumBaseCells  = 122
	h3MaxRes        = 15
	h3DigitBits     = 3
	h3DigitMask     = 0x7
	// The value an UNUSED digit slot carries. It is also not a legal value
	// for a used one, which is why one constant serves both checks.
	h3UnusedDigit = 0x7
)

// IsH3Cell reports whether an index is a structurally well-formed H3 cell: the
// reserved bit is clear, the mode says "cell", the mode-dependent bits are
// clear, the base cell exists, every digit up to the resolution is a real
// direction, and every digit above it is unused.
//
// It does NOT prove the cell exists on the grid — see the pentagon note above.
// What it does prove is enough for the job it has here: the words a rendered
// listing leaks into a pipe ("3", "500", "at", "cell(s)", a geo_id, a cursor)
// are refused, and so is any number that is not shaped like a cell.
func IsH3Cell(index uint64) bool {
	if index>>h3HighBitShift != 0 {
		return false
	}
	if (index>>h3ModeShift)&h3ModeMask != h3CellMode {
		return false
	}
	if (index>>h3ReservedShift)&h3ReservedMask != 0 {
		return false
	}
	if (index>>h3BaseCellShift)&h3BaseCellMask >= h3NumBaseCells {
		return false
	}
	// Resolution needs no bounds check: the field is four bits, so it is
	// already 0..15.
	res := int((index >> h3ResShift) & h3ResMask)

	// Every digit above the resolution is unused, and all of them sit in one
	// run of low bits — so the whole tail is one mask compare rather than a
	// second loop.
	unusedBits := uint((h3MaxRes - res) * h3DigitBits)
	unused := (uint64(1) << unusedBits) - 1
	if index&unused != unused {
		return false
	}
	// Every digit at or below the resolution is a direction, 0..6. 7 is the
	// unused marker and never names one.
	for r := 1; r <= res; r++ {
		shift := uint((h3MaxRes - r) * h3DigitBits)
		if (index>>shift)&h3DigitMask == h3UnusedDigit {
			return false
		}
	}
	return true
}

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
