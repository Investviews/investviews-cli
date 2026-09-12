package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCoverageDecodesTheStalenessFields(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"window_days":365,"freshness":"computed live","coverage":"the markets served",
		 "countries":[
		   {"country":"es","h3_resolutions":[8],"last_ad_parsed_at":"2026-08-19T23:41:07Z",
		    "searchable":true,"prices_available":true,"reports_available":true},
		   {"country":"me","h3_resolutions":[8],"last_ad_parsed_at":null,
		    "searchable":false,"prices_available":true,"reports_available":false}]}`)
	})

	res, err := rec.client.Coverage(context.Background(), CoverageParams{})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if res.WindowDays != 365 || len(res.Countries) != 2 {
		t.Fatalf("res = %+v", res)
	}
	// ⚠️ null last_ad_parsed_at means no source targets that market
	// directly — not that it is empty.
	if res.Countries[1].LastAdParsedAt != nil {
		t.Error("a null staleness field must stay nil, not become an empty string")
	}
	// The gap between prices_available and searchable is the useful part.
	if !res.Countries[1].PricesAvailable || res.Countries[1].Searchable {
		t.Error("a market can hold listings and still name nothing")
	}
}

func TestCoveragePassesTheCountryFilter(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"window_days":365,"freshness":"","coverage":"","countries":[]}`)
	})
	if _, err := rec.client.Coverage(context.Background(), CoverageParams{Country: "es"}); err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if rec.last().Query != "country=es" {
		t.Errorf("query = %q", rec.last().Query)
	}
}

func TestMetaFiltersKeepsTheVocabularyReadable(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"version":"v1","docs_url":"https://docs.investviews.ai/",
		 "range_semantics":"Ranges are inclusive [min, max].",
		 "filters":{
		   "ad_type":{"values":["real_estate_residential","real_estate_commercial"],"description":"x"},
		   "ad_sub_type":{"values":["buy","rent"],"description":"x"},
		   "price":{"grid":"log10","steps_per_decade":40,"base_currency":"USD"}}}`)
	})

	res, err := rec.client.MetaFilters(context.Background())
	if err != nil {
		t.Fatalf("MetaFilters: %v", err)
	}
	values, ok := res.Values("ad_sub_type")
	if !ok || len(values) != 2 || values[0] != "buy" {
		t.Errorf("Values(ad_sub_type) = %v, %v", values, ok)
	}
	// price publishes bin geometry, not a value list — and that is fine.
	if _, ok := res.Values("price"); ok {
		t.Error("price has no values list")
	}
	if _, ok := res.Values("nothing_like_this"); ok {
		t.Error("an unknown filter is not present")
	}
}

// ⚠️ On an uncounted group limit/used/remaining are null, NOT 0: zero would
// read as "no allowance left", the opposite of the truth.
func TestUsageKeepsUncountedGroupsNull(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"token":{"name":"cli","read_only":true,"masked_token":"iv_live_ro_...4f9c",
		  "status":"active","created_at":"2026-07-02T09:14:11Z","last_used_at":null},
		 "groups":{
		   "current":{"metered":true,"limit":100000,"used":3184,"remaining":96816,
		              "resets_at":"2026-09-02T09:14:11Z","rate_per_min":300,
		              "source":"subscription_feature","degraded":false},
		   "metadata":{"metered":false,"limit":null,"used":null,"remaining":null,
		               "resets_at":null,"rate_per_min":120,"source":"constant","degraded":false}}}`)
	})

	res, err := rec.client.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if res.Token.MaskedToken != "iv_live_ro_...4f9c" || !res.Token.ReadOnly {
		t.Errorf("token = %+v", res.Token)
	}
	if res.Token.LastUsedAt != nil {
		t.Error("a null last_used_at stays nil")
	}

	metadata := res.Groups["metadata"]
	if metadata.Metered {
		t.Error("the metadata group is uncounted")
	}
	if metadata.Limit != nil || metadata.Used != nil || metadata.Remaining != nil || metadata.ResetsAt != nil {
		t.Error("an uncounted group reports null, never 0")
	}
	if metadata.RatePerMin != 120 {
		t.Errorf("rate_per_min = %d — uncounted is not unlimited", metadata.RatePerMin)
	}

	current := res.Groups["current"]
	if !current.Metered || current.Limit == nil || *current.Limit != 100000 {
		t.Errorf("current = %+v", current)
	}
}

func TestStatsCurrentDecodesTheEnvelopeAndTheRows(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		meteredHeaders(w)
		writeJSON(w, 200, `{"as_of":"2026-08-01","currency":"EUR","resolution":8,
		 "period":"2026-08-01","granularity":"rolling_3m","window_start":"2026-06-01",
		 "window_end":"2026-08-31","fx_date":"2026-07-15",
		 "territory":{"selector":"geo_id","resolution":null,"hex_count":0,"geo_id":"R14727511",
		   "grain":"geo","place":{"name":"Russafa","level":"microzone","country":"es"}},
		 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy","rooms":null,
		   "min_size":null,"max_size":null,"min_price_usd":150000,"max_price_usd":null,
		   "units":{"size":"m2","price":"USD"},"note":"price filters are USD"},
		 "snapshot":{"as_of":"2026-08-01","built":"monthly","earliest_period":"2024-03-01"},
		 "stats":[{"h3":"613498079267520511","estimated":false,"low_confidence":false,
		   "median_price":285000,"median_price_per_sqm":3120,"avg_area":78.5,
		   "p05_price":150000,"p95_price":520000,"max_price":null,
		   "name":"Russafa","zone_level":"microzone",
		   "zone":{"region":"Comunitat Valenciana","province":null,"city":"València",
		           "macrozone":"l'Eixample","microzone":"Russafa","country":"es"}}]}`)
	})

	res, err := rec.client.StatsCurrent(context.Background(), StatsParams{
		GeoID: "R14727511", Currency: "EUR", MinPriceUSD: ptrFloat(150000),
	})
	if err != nil {
		t.Fatalf("StatsCurrent: %v", err)
	}
	// ⚠️ The figures describe ONE PERIOD, and the window is echoed for
	// citing it. Do not assume it from res.
	if res.Granularity != "rolling_3m" || res.WindowStart == nil || *res.WindowStart != "2026-06-01" {
		t.Errorf("window = %+v", res)
	}
	// ⚠️ hex_count 0 with grain "geo" is not an empty result.
	if res.Territory.HexCount != 0 || res.Territory.Grain != "geo" || res.Territory.Resolution != nil {
		t.Errorf("territory = %+v", res.Territory)
	}
	if res.Territory.Place == nil || res.Territory.Place.Name != "Russafa" {
		t.Errorf("place = %+v", res.Territory.Place)
	}
	// ⚠️ Price filters are USD whatever currency displays.
	if res.Currency != "EUR" || res.Filters.MinPriceUSD == nil || *res.Filters.MinPriceUSD != 150000 {
		t.Errorf("filters = %+v", res.Filters)
	}
	if res.Filters.Units.Price != "USD" {
		t.Errorf("units = %+v", res.Filters.Units)
	}

	row := res.Stats[0]
	if row.MedianPrice == nil || *row.MedianPrice != 285000 {
		t.Errorf("median_price = %v", row.MedianPrice)
	}
	if row.MaxPrice != nil {
		t.Error("a null figure stays nil, not 0")
	}
	if row.Zone == nil || row.Zone.Province != nil {
		t.Error("an absent level in the zone tree stays nil")
	}
	if row.ZoneLevel == nil || *row.ZoneLevel != "microzone" {
		t.Errorf("zone_level = %v", row.ZoneLevel)
	}
	// The wire form of every price filter and the display currency.
	q := rec.last().Query
	for _, want := range []string{"geo_id=R14727511", "currency=EUR", "min_price_usd=150000"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q is missing %q", q, want)
		}
	}
}

// ⚠️ "No data" is a 200 with stats: [], not an error. Rendering it as a
// failure sends the user hunting for a problem that does not exist.
func TestStatsCurrentTreatsAnEmptyResultAsANormalAnswer(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		meteredHeaders(w)
		writeJSON(w, 200, `{"as_of":null,"currency":"USD","resolution":8,
		 "territory":{"selector":"geo_id","resolution":null,"hex_count":0,"geo_id":"jp","grain":"geo"},
		 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy"},
		 "snapshot":{"as_of":null,"built":"monthly","earliest_period":"2024-03-01"},
		 "stats":[]}`)
	})

	res, err := rec.client.StatsCurrent(context.Background(), StatsParams{GeoID: "jp"})
	if err != nil {
		t.Fatalf("an empty result is a 200, not an error: %v", err)
	}
	if res.Stats == nil || len(res.Stats) != 0 {
		t.Errorf("stats = %v, want an empty slice", res.Stats)
	}
	if !res.Meta.Metered() {
		t.Error("an empty answer still spent a metered request — say so")
	}
}

func TestStatsHistoryJoinsCellsAndCarriesTheRange(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		meteredHeaders(w)
		writeJSON(w, 200, `{"currency":"EUR","resolution":8,
		 "territory":{"selector":"h3","resolution":8,"hex_count":2},
		 "filters":{"ad_type":"real_estate_residential","ad_sub_type":"buy",
		   "units":{"size":"m2","price":"USD"}},
		 "series":[
		   {"period":"2025-02-01","granularity":"rolling_3m","window_start":"2024-12-01",
		    "window_end":"2025-02-28","as_of":"2025-02-28","fx_date":"2025-01-15",
		    "stats":[{"h3":"613498079267520511","estimated":false,"median_price":285000}]},
		   {"period":"2025-03-01","granularity":"rolling_3m","window_start":"2025-01-01",
		    "window_end":"2025-03-31","as_of":"2025-03-31","fx_date":"2025-02-15","stats":[]}],
		 "series_note":"Each point is an independent aggregate; points are NOT additive.",
		 "snapshot":{"as_of":"2025-03-01","built":"monthly","earliest_period":"2024-03-01"}}`)
	})

	res, err := rec.client.StatsHistory(context.Background(), HistoryParams{
		StatsParams: StatsParams{H3: []string{"613498079267520511", "613498079269617663"}},
		From:        "2025-02", To: "2025-03",
	})
	if err != nil {
		t.Fatalf("StatsHistory: %v", err)
	}
	if len(res.Series) != 2 {
		t.Fatalf("series = %d", len(res.Series))
	}
	// A period with nothing in it is present with an empty stats array, so
	// a gap in the data is distinguishable from a gap in the range.
	if res.Series[1].Stats == nil || len(res.Series[1].Stats) != 0 {
		t.Error("an empty period stays in the series")
	}
	if !strings.Contains(res.SeriesNote, "NOT additive") {
		t.Errorf("series_note = %q", res.SeriesNote)
	}

	q := rec.last().Query
	if !strings.Contains(q, "h3=613498079267520511%2C613498079269617663") {
		t.Errorf("cells must be comma-joined: %q", q)
	}
	for _, want := range []string{"from=2025-02", "to=2025-03"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q is missing %q", q, want)
		}
	}
}

func TestStatsParamsEncodeRoomsAndSizes(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		meteredHeaders(w)
		writeJSON(w, 200, `{"currency":"USD","stats":[]}`)
	})
	_, err := rec.client.StatsCurrent(context.Background(), StatsParams{
		GeoID:   "es",
		Rooms:   []string{"2", "3", "unknown"},
		MinSize: ptrFloat(40), MaxSize: ptrFloat(120.5),
		AdType: "real_estate_commercial", AdSubType: "rent",
	})
	if err != nil {
		t.Fatalf("StatsCurrent: %v", err)
	}
	q := rec.last().Query
	for _, want := range []string{
		"rooms=2%2C3%2Cunknown", "min_size=40", "max_size=120.5",
		"ad_type=real_estate_commercial", "ad_sub_type=rent",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q is missing %q", q, want)
		}
	}
}

func TestGeoLookupSelectorRules(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"kind":"point","lat":39.4699,"lng":-0.3763,"h3":"613498079267520511",
		 "h3_res":8,"source":"coordinates","country":"es","display_name":null,"zones":[],
		 "prices_available":true,"reports_available":true}`)
	})
	ctx := context.Background()

	if _, err := rec.client.GeoLookup(ctx, LookupParams{}); err == nil {
		t.Error("a lookup with neither a cell nor a point must be refused")
	}
	if _, err := rec.client.GeoLookup(ctx, LookupParams{H3: "1", Lat: ptrFloat(1), Lng: ptrFloat(2)}); err == nil {
		t.Error("a lookup naming both a cell and a point must be refused")
	}
	if _, err := rec.client.GeoLookup(ctx, LookupParams{Lat: ptrFloat(1)}); err == nil {
		t.Error("half a coordinate pair must be refused")
	}
	if rec.hits() != 0 {
		t.Errorf("hits = %d — local refusals cost no request", rec.hits())
	}

	point, err := rec.client.GeoLookup(ctx, LookupParams{Lat: ptrFloat(39.4699), Lng: ptrFloat(-0.3763), Res: ptrInt(8)})
	if err != nil {
		t.Fatalf("GeoLookup: %v", err)
	}
	if point.H3 != "613498079267520511" {
		t.Errorf("h3 = %q", point.H3)
	}
	q := rec.last().Query
	for _, want := range []string{"lat=39.4699", "lng=-0.3763", "res=8"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q is missing %q", q, want)
		}
	}

	// A cell carries its own resolution, so res is not sent alongside it.
	if _, err := rec.client.GeoLookup(ctx, LookupParams{H3: "613498079267520511", Res: ptrInt(4)}); err != nil {
		t.Fatalf("GeoLookup: %v", err)
	}
	if strings.Contains(rec.last().Query, "res=") {
		t.Errorf("res must not be sent with h3: %q", rec.last().Query)
	}
}

func TestGeoSearchNeedsAName(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})
	if _, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "  "}); err == nil {
		t.Fatal("an empty query must be refused locally")
	}
	if rec.hits() != 0 {
		t.Errorf("hits = %d", rec.hits())
	}
}

func TestPlaceHexesNeedsAGeoID(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})
	if _, err := rec.client.PlaceHexes(context.Background(), " ", HexesParams{}); err == nil {
		t.Fatal("an empty geo_id must be refused locally")
	}
}

// An empty results array is a normal /geo answer — a level a branch does not
// have, a page past the end, an unresolvable parent. Never an error.
func TestGeoBrowseTreatsAnEmptyBranchAsANormalAnswer(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, `{"parent":{"geo_id":"R344953","name":"València","level":"city",
		 "country":"es","ancestors":[]},"results":[]}`)
	})
	res, err := rec.client.GeoBrowse(context.Background(), GeoParams{Parent: "R344953", Level: "microzone"})
	if err != nil {
		t.Fatalf("an empty branch is not an error: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("results = %d", len(res.Results))
	}
	if res.Parent == nil || res.Parent.GeoID != "R344953" {
		t.Error("the parent object is still present on an empty branch")
	}
}
