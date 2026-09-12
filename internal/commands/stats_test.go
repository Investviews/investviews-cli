package commands

import (
	"net/http"
	"strings"
	"testing"
)

const currentBody = `{"as_of":"2026-08-31","currency":"EUR","resolution":null,"period":"2026-08-01",
 "granularity":"month","window_start":"2026-08-01","window_end":"2026-08-31","fx_date":"2026-08-15",
 "territory":{"selector":"geo_id","resolution":null,"hex_count":0,"geo_id":"R344953","grain":"geo",
   "place":{"name":"València","level":"city","country":"es"}},
 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy","rooms":[],
   "min_size":null,"max_size":null,"min_price_usd":null,"max_price_usd":null,
   "units":{"size":"m2","price":"USD"},"note":"Price filters are USD."},
 "snapshot":{"as_of":"2026-08-01","built":"2026-09-05","earliest_period":"2024-03-01"},
 "stats":[{"h3":"","estimated":false,"low_confidence":false,"name":"València","zone_level":"city",
   "median_price_per_sqm":3120,"median_price":285000,"median_area":92,
   "p05_price_per_sqm":1800,"p95_price_per_sqm":5200}]}`

// ⚠️ ONE TERRITORY PER REQUEST, refused LOCALLY. The point is not only the
// message: it is that nothing was sent, so the mistake cost no quota.
func TestStatsCurrentRefusesTwoSelectorsWithoutSpendingARequest(t *testing.T) {
	cases := [][]string{
		{"stats", "current", "--geo-id", "R344953", "--h3", "613498076398616575"},
		{"stats", "current", "--geo-id", "R344953", "--lat", "39.4", "--lng", "-0.3", "--radius-km", "5"},
		{"stats", "history", "--geo-id", "es", "--h3", "1,2"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := run(t, nil, args...)
			if res.err == nil {
				t.Fatal("two selectors must be refused")
			}
			if res.hits() != 0 {
				t.Errorf("a locally refused request sent %d requests; it must send none", res.hits())
			}
			contains(t, res.err.Error(), "a request names ONE territory")
			contains(t, res.err.Error(), "drop all but one")
			contains(t, res.err.Error(), "cost no quota")
		})
	}
}

func TestStatsCurrentRequiresATerritoryAndSaysHowToGetOne(t *testing.T) {
	res := run(t, nil, "stats", "current")
	if res.err == nil {
		t.Fatal("no selector must be refused")
	}
	if res.hits() != 0 {
		t.Errorf("sent %d requests", res.hits())
	}
	contains(t, res.err.Error(), "investviews geo search")
	contains(t, res.err.Error(), "both are free")
}

func TestStatsCurrentRefusesAHalfNamedCircle(t *testing.T) {
	res := run(t, nil, "stats", "current", "--lat", "39.4", "--lng", "-0.3")
	if res.err == nil {
		t.Fatal("a circle missing its radius must be refused")
	}
	contains(t, res.err.Error(), "all three of --lat, --lng and --radius-km")
	if res.hits() != 0 {
		t.Errorf("sent %d requests", res.hits())
	}
}

func TestStatsCurrentSendsEveryFilterItWasGiven(t *testing.T) {
	res := run(t, meteredJSON(currentBody),
		"stats", "current",
		"--h3", "613498076398616575,613498076398616576", "--h3", "613498076398616577",
		"--rooms", "2,3", "--ad-type", "real_estate_commercial", "--ad-sub-type", "rent",
		"--min-size", "40", "--max-size", "120",
		"--min-price-usd", "150000", "--max-price-usd", "400000",
		"--currency", "EUR")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}

	query := res.lastQuery()
	for _, want := range []string{
		"h3=613498076398616575%2C613498076398616576%2C613498076398616577",
		"rooms=2%2C3",
		"ad_type=real_estate_commercial",
		"ad_sub_type=rent",
		"min_size=40", "max_size=120",
		"min_price_usd=150000", "max_price_usd=400000",
		"currency=EUR",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q is missing %q", query, want)
		}
	}
	// ⚠️ res is refused alongside h3 by the client; it must not appear here
	// either, since the flag was never given.
	if strings.Contains(query, "res=") {
		t.Errorf("query %q sent a resolution nobody asked for", query)
	}
}

// --radius is an accepted spelling of --radius-km. The API's parameter keeps
// its unit in the name.
func TestStatsCurrentAcceptsRadiusAsAnAliasForRadiusKm(t *testing.T) {
	res := run(t, meteredJSON(currentBody),
		"stats", "current", "--lat", "39.4699", "--lng", "-0.3763", "--radius", "5")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	if !strings.Contains(res.lastQuery(), "radius_km=5") {
		t.Errorf("query = %q", res.lastQuery())
	}
}

func TestStatsCurrentReadsCellsFromStandardInput(t *testing.T) {
	stdin := strings.NewReader("613498076398616575\n613498076398616576\n\n")
	res := runWithStdin(t, meteredJSON(currentBody), stdin, "stats", "current", "--h3", "-")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	if !strings.Contains(res.lastQuery(), "h3=613498076398616575%2C613498076398616576") {
		t.Errorf("query = %q", res.lastQuery())
	}
}

// ⚠️ A free call and a metered one must not look alike, and the SERVER is what
// decides which happened.
func TestStatsCurrentMarksTheCallAsMeteredFromTheServersHeaders(t *testing.T) {
	res := run(t, meteredJSON(currentBody), "stats", "current", "--geo-id", "R344953")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	contains(t, res.stdout, "cost: METERED — group current · 1 request · 138 of 100000 used this cycle")
	missing(t, res.stdout, "cost: FREE")
}

func TestFreeCommandsAreMarkedFree(t *testing.T) {
	res := run(t, freeJSON(countriesBody), "geo", "browse")
	if res.err != nil {
		t.Fatalf("browse: %v", res.err)
	}
	contains(t, res.stdout, "cost: FREE — group metadata, uncounted · 1 request")
	missing(t, res.stdout, "METERED")
}

// Every text answer states the period, its window, the as_of date and the
// place, because those are what an agent has to cite.
func TestStatsCurrentStatesTheWindowAndThePlace(t *testing.T) {
	res := run(t, meteredJSON(currentBody), "stats", "current", "--geo-id", "R344953")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	contains(t, res.stdout, "València (city, es) — geo_id R344953")
	contains(t, res.stdout, "period 2026-08-01 (month) · window 2026-08-01 → 2026-08-31 · as_of 2026-08-31")
	contains(t, res.stdout, "filters: real_estate_residential / buy")
	contains(t, res.stdout, "price filters are USD whatever the display currency is")
	contains(t, res.stdout, "3120")
	contains(t, res.stdout, "285000")
}

// ⚠️ "No data" is a 200 with an empty array. It is a normal answer, exits 0,
// and names the next move — which is history, not despair.
func TestStatsCurrentEmptyIsANormalAnswer(t *testing.T) {
	body := `{"as_of":"2026-08-31","currency":"USD","period":"2026-08-01","granularity":"month",
	 "window_start":"2026-08-01","window_end":"2026-08-31","fx_date":"2026-08-15",
	 "territory":{"selector":"geo_id","hex_count":0,"geo_id":"jp","grain":"geo"},
	 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy","rooms":[],
	   "units":{"size":"m2","price":"USD"},"note":""},
	 "snapshot":{"as_of":"2026-08-01","built":"2026-09-05","earliest_period":"2024-03-01"},
	 "stats":[]}`

	res := run(t, meteredJSON(body), "stats", "current", "--geo-id", "jp")
	if res.err != nil {
		t.Fatalf("an empty answer must not be an error, got: %v", res.err)
	}
	contains(t, res.stdout, "That is a NORMAL answer, not an")
	contains(t, res.stdout, "History goes back to 2024-03-01.")
	contains(t, res.stdout, "Next: investviews stats history --geo-id jp")
}

// ⚠️ ABSENT IS NOT FALSE. An unstamped low_confidence says nothing either way,
// so it must never render as a clean bill of health.
func TestStatsCurrentDoesNotReadAnAbsentLowConfidenceAsConfident(t *testing.T) {
	body := strings.Replace(currentBody, `"low_confidence":false`, `"low_confidence":null`, 1)
	res := run(t, meteredJSON(body), "stats", "current", "--geo-id", "R344953")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	contains(t, res.stdout, "absent means UNSTAMPED")
}

func TestStatsCurrentFlagsEstimatedRows(t *testing.T) {
	body := strings.Replace(currentBody, `"estimated":false`, `"estimated":true`, 1)
	res := run(t, meteredJSON(body), "stats", "current", "--geo-id", "R344953")
	if res.err != nil {
		t.Fatalf("stats current: %v", res.err)
	}
	contains(t, res.stdout, "estimated")
	contains(t, res.stdout, "pooled from the")
}

// --json must leave stdout as nothing but the payload, so it pipes into jq.
func TestJSONModeKeepsTheCostLineOffStdout(t *testing.T) {
	res := run(t, meteredJSON(currentBody), "stats", "current", "--geo-id", "R344953", "--json")
	if res.err != nil {
		t.Fatalf("stats current --json: %v", res.err)
	}
	if !strings.HasPrefix(strings.TrimSpace(res.stdout), "{") {
		t.Errorf("stdout is not a JSON document:\n%s", res.stdout)
	}
	missing(t, res.stdout, "cost:")
	contains(t, res.stderr, "cost: METERED — group current")
	contains(t, res.stdout, `"median_price": 285000`)
}

// ─────────────────────────────────────────────────────────────────────────────
// stats history
// ─────────────────────────────────────────────────────────────────────────────

const historyBody = `{"currency":"EUR","resolution":8,
 "territory":{"selector":"geo_id","resolution":8,"hex_count":0,"geo_id":"R344953","grain":"geo",
   "place":{"name":"València","level":"city","country":"es"}},
 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy","rooms":[],
   "units":{"size":"m2","price":"USD"},"note":""},
 "series":[
   {"period":"2025-02-01","granularity":"rolling_3m","window_start":"2024-12-01",
    "window_end":"2025-02-28","as_of":"2025-02-28","fx_date":"2025-01-15",
    "stats":[{"h3":"613498079267520511","estimated":false,"median_price":285000,
      "median_price_per_sqm":3120,"median_area":91}]},
   {"period":"2025-03-01","granularity":"rolling_3m","window_start":"2025-01-01",
    "window_end":"2025-03-31","as_of":"2025-03-31","fx_date":"2025-02-15",
    "stats":[{"h3":"613498079267520511","estimated":false,"median_price":291500,
      "median_price_per_sqm":3190,"median_area":92}]}],
 "series_note":"Each point is an independent aggregate over its own window; points are NOT additive.",
 "snapshot":{"as_of":"2025-03-01","built":"2025-04-05","earliest_period":"2024-03-01"}}`

func TestStatsHistoryPrintsTheSeriesAndRefusesToLookAdditive(t *testing.T) {
	res := run(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Quota-Group", "history")
		w.Header().Set("X-Quota-Limit", "20000")
		w.Header().Set("X-Quota-Used", "12")
		writeBody(w, http.StatusOK, historyBody)
	}, "stats", "history", "--geo-id", "R344953", "--from", "2025-02", "--to", "2025-03")

	if res.err != nil {
		t.Fatalf("stats history: %v", res.err)
	}
	contains(t, res.stdout, "2 period(s)")
	contains(t, res.stdout, "2025-02-01")
	contains(t, res.stdout, "2024-12-01 → 2025-02-28")
	contains(t, res.stdout, "POINTS ARE NOT ADDITIVE")
	contains(t, res.stdout, "points are NOT additive.")
	// history bills a SEPARATE budget from current, and the cost line says so.
	contains(t, res.stdout, "cost: METERED — group history")

	query := res.lastQuery()
	if !strings.Contains(query, "from=2025-02") || !strings.Contains(query, "to=2025-03") {
		t.Errorf("query = %q", query)
	}
}

// A period holding several cells has no single figure, and averaging them here
// would invent one.
func TestStatsHistoryDoesNotInventAFigureForAMultiCellPeriod(t *testing.T) {
	body := strings.Replace(historyBody,
		`"stats":[{"h3":"613498079267520511","estimated":false,"median_price":285000,
      "median_price_per_sqm":3120,"median_area":91}]`,
		`"stats":[{"h3":"1","estimated":false,"median_price":285000,"median_price_per_sqm":3120},
		          {"h3":"2","estimated":false,"median_price":310000,"median_price_per_sqm":3400}]`, 1)

	res := run(t, meteredJSON(body), "stats", "history", "--geo-id", "R344953")
	if res.err != nil {
		t.Fatalf("stats history: %v", res.err)
	}
	contains(t, res.stdout, "ROWS")
	contains(t, res.stdout, "no single figure per period")
	missing(t, res.stdout, "MEDIAN PRICE")
}

func TestStatsHistoryEmptyRangeIsANormalAnswer(t *testing.T) {
	body := `{"currency":"USD","territory":{"selector":"geo_id","hex_count":0,"geo_id":"es"},
	 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy","rooms":[],
	   "units":{"size":"m2","price":"USD"},"note":""},
	 "series":[],"series_note":"",
	 "snapshot":{"as_of":null,"built":"","earliest_period":"2024-03-01"}}`

	res := run(t, meteredJSON(body), "stats", "history", "--geo-id", "es", "--from", "2020-01")
	if res.err != nil {
		t.Fatalf("an empty series must not be an error, got: %v", res.err)
	}
	contains(t, res.stdout, "No periods in this range. That is a NORMAL answer")
	contains(t, res.stdout, "History goes back to 2024-03-01")
}
