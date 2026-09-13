package api

import (
	"errors"
	"strings"
	"testing"
)

// ⚠️ THE TOKENS IN THIS TABLE ARE NOT INVENTED. Everything under "a rendered
// listing, piped" is a real word from a real `investviews geo hexes` run —
// header, availability sentence, cursor hint and cost line — measured against
// the live API on 2026-09-13. They are what `--h3 -` was being handed, and
// they are why this parser cannot be a digit test: "3", "8" and "500" are
// perfectly good decimal numbers taken straight out of "3 cell(s)", "at res 8"
// and "500 cell(s)".
func TestParseH3Cell(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  bool
	}{
		// the two documented forms
		{"decimal id, as every endpoint emits", "613498076398616575", true},
		{"decimal id, res 8 in València", "613498079194120191", true},
		{"canonical H3 string", "8839540ad1fffff", true},
		{"canonical H3 string, upper case", "8839540AD1FFFFF", true},
		{"surrounding whitespace is not a value", "  613498076398616575\n", true},

		// a rendered listing, piped
		{"a geo_id from the header", "R344953", false},
		{"a place name from the header", "València", false},
		{"a level, bracketed", "(city,", false},
		{"a country, bracketed", "es)", false},
		{"an em dash", "—", false},
		{"the cell COUNT from the header", "3", false},
		{"the word cell(s)", "cell(s)", false},
		{"the word at", "at", false},
		{"the RESOLUTION from the header", "8", false},
		{"a page size from the header", "500", false},
		{"a cursor from the more-cells hint", "eyJrIjoxfQ", false},
		{"the word cost:", "cost:", false},
		{"FREE from the cost line", "FREE", false},

		// near misses
		{"empty", "", false},
		{"zero", "0", false},
		{"negative", "-613498076398616575", false},
		{"above uint64", "99999999999999999999999", false},
		{"0x prefix", "0x8839540ad1fffff", false},
		{"an H3 index with the wrong mode bits", "1229782938247303441", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseH3Cell(tc.token)
			if tc.want != (err == nil) {
				t.Fatalf("ParseH3Cell(%q) error = %v, want accepted = %v", tc.token, err, tc.want)
			}
		})
	}
}

// Both documented forms name the SAME cell, so they must parse to the same
// index — otherwise "accepted" would mean two different things.
func TestParseH3CellAgreesAcrossTheTwoForms(t *testing.T) {
	decimal, err := ParseH3Cell("613498079267520511")
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	// The pair the contract itself prints for this cell.
	canonical, err := ParseH3Cell("8839540ad1fffff")
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if decimal != canonical {
		t.Errorf("the two forms of one cell parsed to %d and %d", decimal, canonical)
	}
}

// The message has to name the token: a piped listing fails on its first word,
// and which word it was is what tells the caller which pipe produced it.
func TestValidateH3CellsNamesTheOffendingTokenAndTheFix(t *testing.T) {
	err := ValidateH3Cells([]string{"613498076398616575", "cell(s)", "613498076398616576"})
	if err == nil {
		t.Fatal("a non-cell token must be refused")
	}
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("err = %T, want *ValidationError", err)
	}
	if valErr.Parameter != "h3" {
		t.Errorf("Parameter = %q, want h3", valErr.Parameter)
	}
	for _, want := range []string{`"cell(s)"`, "cost no quota", "--ids-only"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message does not mention %q:\n%s", want, err.Error())
		}
	}
	if ExitCode(err) != ExitError {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitError)
	}
}

func TestValidateH3CellsAcceptsAWholeListOfRealCells(t *testing.T) {
	cells := []string{"613498076398616575", "613498076402810879", "8839540ad1fffff"}
	if err := ValidateH3Cells(cells); err != nil {
		t.Fatalf("ValidateH3Cells(%v) = %v", cells, err)
	}
	if err := ValidateH3Cells(nil); err != nil {
		t.Errorf("an empty list is not a bad cell: %v", err)
	}
}
