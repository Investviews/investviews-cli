package commands

import (
	"strings"
	"testing"
)

const coverageBody = `{"window_days":365,"freshness":"computed live per request",
 "coverage":"the markets this API serves",
 "countries":[
   {"country":"es","h3_resolutions":[4,5,6,7,8],"last_ad_parsed_at":"2026-09-11T23:41:07Z",
    "searchable":true,"prices_available":true,"reports_available":true},
   {"country":"me","h3_resolutions":[8],"last_ad_parsed_at":null,
    "searchable":false,"prices_available":true,"reports_available":false}]}`

// ⚠️ prices_available and searchable answer different questions, and the gap
// between them is the useful part: a market can hold listings and still name
// nothing.
func TestCoverageKeepsPricesAndSearchableApart(t *testing.T) {
	res := run(t, freeJSON(coverageBody), "coverage")
	if res.err != nil {
		t.Fatalf("coverage: %v", res.err)
	}
	contains(t, res.stdout, "2 market(s), measured over a 365-day window.")

	es := rowFor(t, res.stdout, "es")
	contains(t, es, "4,5,6,7,8")

	me := rowFor(t, res.stdout, "me")
	// prices yes, searchable no — the two columns must disagree.
	contains(t, me, "yes")
	contains(t, me, "no")
	// ⚠️ A blank last-parsed date means no source targets that market
	// directly, NOT that it is empty.
	contains(t, me, "—")
	contains(t, res.stdout, "means no source targets that market directly — not empty")
}

func TestCoverageIsFreeAndPassesItsCountryFilter(t *testing.T) {
	res := run(t, freeJSON(coverageBody), "coverage", "--country", "es")
	if res.err != nil {
		t.Fatalf("coverage: %v", res.err)
	}
	if res.lastQuery() != "country=es" {
		t.Errorf("query = %q", res.lastQuery())
	}
	contains(t, res.stdout, "cost: FREE — group metadata, uncounted")
}

// A valid code with no row is the honest answer to a well-formed question, not
// an error.
func TestCoverageEmptyForAValidCodeIsANormalAnswer(t *testing.T) {
	body := `{"window_days":365,"freshness":"","coverage":"","countries":[]}`
	res := run(t, freeJSON(body), "coverage", "--country", "aq")
	if res.err != nil {
		t.Fatalf("an empty coverage list must not be an error, got: %v", res.err)
	}
	contains(t, res.stdout, `No row for "aq"`)
	contains(t, res.stdout, "honest answer to a well-formed question")
}

func TestCoverageJSONModeIsPipeable(t *testing.T) {
	res := run(t, freeJSON(coverageBody), "coverage", "--json")
	if res.err != nil {
		t.Fatalf("coverage --json: %v", res.err)
	}
	contains(t, res.stdout, `"window_days": 365`)
	missing(t, res.stdout, "cost:")
	contains(t, res.stderr, "cost: FREE")
}

// rowFor finds the table row for one country. It anchors on the start of the
// line: "es" also appears inside "computed live per request", and a looser
// match would test the prose instead of the table.
func rowFor(t *testing.T, body, country string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, country+" ") {
			return line
		}
	}
	t.Fatalf("no table row for %q\n%s", country, body)
	return ""
}
