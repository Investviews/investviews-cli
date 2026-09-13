package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ⚠️ ancestors: null and ancestors: [] are DIFFERENT ANSWERS.
//
//	[]   — a country at the root: STOP walking up.
//	null — we hold no place for that zone: TRY A DIFFERENT SELECTOR.
//
// A plain []Ancestor collapses both to a nil slice.
func TestAncestorsKeepsNullApartFromEmpty(t *testing.T) {
	var empty Ancestors
	if err := json.Unmarshal([]byte(`[]`), &empty); err != nil {
		t.Fatalf("[]: %v", err)
	}
	if !empty.Known() {
		t.Error("[] is a known answer")
	}
	if !empty.Root() {
		t.Error("[] means the place has no ancestors")
	}
	if empty.Len() != 0 {
		t.Errorf("Len = %d", empty.Len())
	}

	var null Ancestors
	if err := json.Unmarshal([]byte(`null`), &null); err != nil {
		t.Fatalf("null: %v", err)
	}
	if null.Known() {
		t.Error("null means we cannot say, not that the answer is known")
	}
	if null.Root() {
		t.Error("null must NOT read as the root answer — that is the whole trap")
	}

	// The two must not be equal in any way a caller might test.
	if empty.Known() == null.Known() {
		t.Error("Known() cannot tell [] from null")
	}

	var absent Ancestors // key missing from the payload: UnmarshalJSON never runs
	if absent.Known() || absent.Root() {
		t.Error("an absent key reads as unknown, like null")
	}
}

func TestAncestorsDecodesTheEntriesAndTheirIDs(t *testing.T) {
	var a Ancestors
	body := `[{"geo_id":"R349000","name":"València / Valencia","level":"province"},
	          {"geo_id":"es","name":"ES","level":"country"}]`
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !a.Known() || a.Root() {
		t.Error("a populated ancestry is known and is not the root")
	}
	if a.Len() != 2 || a.List()[0].GeoID != "R349000" {
		t.Fatalf("List = %+v", a.List())
	}
	country, ok := a.Country()
	if !ok || country.GeoID != "es" {
		t.Errorf("Country = %+v, %v — the chain ends on the country", country, ok)
	}
}

// ⚠️ ancestors was a comma-separated STRING until 2026-09-10. A client that
// types it as a string compiles, runs, and silently loses every id, so a bare
// string must be a DECODE ERROR here.
func TestAncestorsRejectsTheOldCommaSeparatedString(t *testing.T) {
	var a Ancestors
	err := json.Unmarshal([]byte(`"València, Comunitat Valenciana, es"`), &a)
	if err == nil {
		t.Fatal("a bare string must not decode: that is the pre-2026-09-10 shape")
	}
	if !strings.Contains(err.Error(), "array") {
		t.Errorf("the error should say an array is expected: %v", err)
	}
	if a.Known() {
		t.Error("a failed decode must not leave the value looking known")
	}
}

func TestAncestorsRoundTripsThroughJSON(t *testing.T) {
	cases := map[string]Ancestors{
		`null`: UnknownAncestors(),
		`[]`:   NewAncestors(nil),
		`[{"geo_id":"es","name":"ES","level":"country"}]`: NewAncestors([]Ancestor{
			{GeoID: "es", Name: "ES", Level: "country"},
		}),
	}
	for want, value := range cases {
		got, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(got) != want {
			t.Errorf("marshal = %s, want %s", got, want)
		}
	}
}

// The asymmetry is the trap: /geo and /geo/search are NEVER null; it is null
// only inside a /geo/lookup point's zones[]. Both branches are exercised here,
// and the null one has to be a synthetic fixture — live sampling of 21 city
// points and 300 random points produced zero nulls.
func TestAncestorsNullOnlyAppearsOnALookupZone(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		switch {
		case strings.HasSuffix(r.URL.Path, "/geo/lookup"):
			writeJSON(w, 200, `{
			  "kind":"point","lat":39.4699,"lng":-0.3763,"h3":"613498079267520511","h3_res":8,
			  "source":"h3","country":"es","display_name":null,
			  "zones":[
			    {"level":"city","name":"València","geo_id":"R344953","current_period":"2026-08-01",
			     "hexes_url":"/public/v1/geo/R344953/hexes",
			     "ancestors":[{"geo_id":"es","name":"ES","level":"country"}]},
			    {"level":"microzone","name":"Some unmapped zone","geo_id":null,
			     "current_period":null,"hexes_url":null,"ancestors":null}
			  ],
			  "prices_available":true,"reports_available":true}`)
		case strings.HasSuffix(r.URL.Path, "/geo/search"):
			writeJSON(w, 200, `{"query":"valencia","results":[
			  {"kind":"zone","geo_id":"R344953","name":"València","level":"city","country":"es",
			   "ancestors":[{"geo_id":"es","name":"ES","level":"country"}],
			   "h3_res":8,"hexes_url":"/public/v1/geo/R344953/hexes",
			   "prices_available":true,"current_period":"2026-08-01","reports_available":true}]}`)
		default:
			writeJSON(w, 200, `{"results":[
			  {"kind":"country","geo_id":"es","name":"ES","level":"country","country":"es",
			   "ancestors":[],"prices_available":true,"current_period":"2026-08-01"}]}`)
		}
	})
	ctx := context.Background()

	// /geo root: [] — known, and the root.
	geo, err := rec.client.GeoBrowse(ctx, GeoParams{})
	if err != nil {
		t.Fatalf("GeoBrowse: %v", err)
	}
	if !geo.Results[0].Ancestors.Known() || !geo.Results[0].Ancestors.Root() {
		t.Error("a country row carries [] — known, and the root")
	}

	// /geo/search: never null.
	search, err := rec.client.GeoSearch(ctx, SearchParams{Q: "valencia"})
	if err != nil {
		t.Fatalf("GeoSearch: %v", err)
	}
	if !search.Results[0].Ancestors.Known() {
		t.Error("/geo/search is never null")
	}

	// /geo/lookup: this is where null lives.
	point, err := rec.client.GeoLookup(ctx, LookupParams{H3: "613498079267520511"})
	if err != nil {
		t.Fatalf("GeoLookup: %v", err)
	}
	if len(point.Zones) != 2 {
		t.Fatalf("zones = %d", len(point.Zones))
	}
	if !point.Zones[0].Ancestors.Known() || !point.Zones[0].Addressable() {
		t.Error("the mapped zone is known and addressable")
	}
	if point.Zones[1].Ancestors.Known() {
		t.Error("the unmapped zone's ancestors are null: we cannot say")
	}
	if point.Zones[1].Ancestors.Root() {
		t.Error("null must not read as the root answer")
	}
	if point.Zones[1].Addressable() {
		t.Error("a zone with geo_id null carries no id to spend")
	}
}

// ⚠️ current_period null means "nothing in the CURRENT window", NOT "no data
// ever" — /stats/history may still answer. Absent is a third state again.
func TestCurrentPeriodKeepsNullApartFromAbsent(t *testing.T) {
	var withDate struct {
		CurrentPeriod NullableDate `json:"current_period"`
	}
	if err := json.Unmarshal([]byte(`{"current_period":"2026-08-01"}`), &withDate); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !withDate.CurrentPeriod.Present() || !withDate.CurrentPeriod.Valid() {
		t.Error("a date is present and valid")
	}
	if withDate.CurrentPeriod.Value() != "2026-08-01" {
		t.Errorf("Value = %q", withDate.CurrentPeriod.Value())
	}

	var null struct {
		CurrentPeriod NullableDate `json:"current_period"`
	}
	if err := json.Unmarshal([]byte(`{"current_period":null}`), &null); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !null.CurrentPeriod.Present() {
		t.Error("an explicit null was present in the payload")
	}
	if null.CurrentPeriod.Valid() {
		t.Error("null carries no date")
	}
	if !null.CurrentPeriod.IsNull() {
		t.Error("IsNull must separate the explicit null from an absent key")
	}

	var absent struct {
		CurrentPeriod NullableDate `json:"current_period"`
	}
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if absent.CurrentPeriod.Present() {
		t.Error("an absent key is not present")
	}
	if absent.CurrentPeriod.IsNull() {
		t.Error("absent is not the same answer as null")
	}
}

func TestQueryableReadsPricesAvailableBesideCurrentPeriod(t *testing.T) {
	cases := []struct {
		name  string
		row   GeoResult
		query bool
	}{
		{"prices and a window", GeoResult{PricesAvailable: true, CurrentPeriod: Date("2026-08-01")}, true},
		{"prices, nothing this period", GeoResult{PricesAvailable: true, CurrentPeriod: NullDate()}, false},
		{"dead end", GeoResult{PricesAvailable: false, CurrentPeriod: NullDate()}, false},
	}
	for _, tc := range cases {
		if got := tc.row.Queryable(); got != tc.query {
			t.Errorf("%s: Queryable = %v, want %v", tc.name, got, tc.query)
		}
	}
}

// ⚠️ low_confidence absent is NOT false — test the key, never the value.
func TestLowConfidenceAbsenceIsDistinctFromFalse(t *testing.T) {
	var absent Stat
	if err := json.Unmarshal([]byte(`{"h3":"613498079267520511","estimated":false}`), &absent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if absent.LowConfidence != nil {
		t.Error("a multi-cell map request omits low_confidence entirely")
	}

	var stamped Stat
	if err := json.Unmarshal([]byte(`{"h3":"1","estimated":false,"low_confidence":false}`), &stamped); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stamped.LowConfidence == nil || *stamped.LowConfidence {
		t.Errorf("LowConfidence = %v, want an explicit false", stamped.LowConfidence)
	}
}

// Hex ids are decimal int64 STRINGS. A client that typed them as numbers would
// lose the low bits of a UInt64.
func TestHexIDsStayStrings(t *testing.T) {
	var page HexPage
	body := `{"geo_id":"R14727511","name":"Russafa","level":"microzone","country":"es","h3_res":8,
	          "hexes":["613498079267520511","613498079269617663"],"next_cursor":null,
	          "prices_available":true,"reports_available":true}`
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Hexes[0] != "613498079267520511" {
		t.Errorf("hex = %q, must round-trip byte for byte", page.Hexes[0])
	}
	if page.HasMore() {
		t.Error("next_cursor null is the end of the listing")
	}
}
