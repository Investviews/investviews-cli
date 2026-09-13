package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// The three models are three different things and the table says so.
func TestPagingDescriptionsMatchTheMeasuredAPI(t *testing.T) {
	if got := GeoPaging(); got.Mode != PageOffset || got.MaxLimit != 100 || got.DefaultLimit != 50 || got.MaxPage != 10000 {
		t.Errorf("GeoPaging = %+v", got)
	}
	if got := SearchPaging(); got.Mode != PageNone || got.DefaultLimit != 10 || got.ServerClamp != 50 {
		t.Errorf("SearchPaging = %+v", got)
	}
	if got := HexesPaging(); got.Mode != PageCursor || got.MaxLimit != 500 || got.DefaultLimit != 500 {
		t.Errorf("HexesPaging = %+v", got)
	}
	if SearchPaging().SupportsAll() {
		t.Error("search cannot be paged, so --all must not be offered for it")
	}
	if !GeoPaging().SupportsAll() || !HexesPaging().SupportsAll() {
		t.Error("offset and cursor paging both support --all")
	}
}

// ── model 1: /geo, offset paging with NO metadata ────────────────────────────

// The response carries no total, no has_more and no page echo, so the walk
// ends on a short or empty page and on nothing else.
func TestGeoBrowseAllPagesUntilAShortPageArrives(t *testing.T) {
	// 250 rows at a limit of 100: pages of 100, 100, 50 — the third is short.
	const total = 250
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		freeHeaders(w)
		writeJSON(w, 200, geoPageBody(total, limit, page))
	})

	var pages int
	var rows []GeoResult
	err := rec.client.GeoBrowseAll(context.Background(), GeoParams{Parent: "es", Level: "city"},
		func(p *GeoResponse) error {
			pages++
			rows = append(rows, p.Results...)
			return nil
		})
	if err != nil {
		t.Fatalf("GeoBrowseAll: %v", err)
	}
	if pages != 3 || len(rows) != total {
		t.Fatalf("pages = %d rows = %d, want 3 and %d", pages, len(rows), total)
	}
	if rec.hits() != 3 {
		t.Errorf("hits = %d — a short page ends the walk, no extra probe", rec.hits())
	}
	if rows[0].GeoID != "R0" || rows[total-1].GeoID != fmt.Sprintf("R%d", total-1) {
		t.Errorf("rows are out of order: %q … %q", rows[0].GeoID, rows[total-1].GeoID)
	}
	// The page number must actually advance; the parent and level must not
	// be dropped along the way.
	if q := rec.requests[1].Query; !strings.Contains(q, "page=2") || !strings.Contains(q, "parent=es") || !strings.Contains(q, "level=city") {
		t.Errorf("second request query = %q", q)
	}
}

func TestGeoBrowseAllStopsOnAnEmptyPage(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		freeHeaders(w)
		// Exactly 200 rows at a limit of 100: page 3 is empty, not short.
		writeJSON(w, 200, geoPageBody(200, limit, page))
	})

	var pages int
	if err := rec.client.GeoBrowseAll(context.Background(), GeoParams{}, func(*GeoResponse) error {
		pages++
		return nil
	}); err != nil {
		t.Fatalf("GeoBrowseAll: %v", err)
	}
	if pages != 2 {
		t.Errorf("pages = %d, want 2 — the empty third page is not handed to the caller", pages)
	}
	if rec.hits() != 3 {
		t.Errorf("hits = %d, want 3 — with no metadata, the empty page IS the end signal", rec.hits())
	}
}

func TestGeoBrowseCollectReturnsEveryRow(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		freeHeaders(w)
		writeJSON(w, 200, geoPageBody(120, limit, page))
	})
	rows, meta, err := rec.client.GeoBrowseCollect(context.Background(), GeoParams{Parent: "es"})
	if err != nil {
		t.Fatalf("GeoBrowseCollect: %v", err)
	}
	if len(rows) != 120 {
		t.Errorf("rows = %d, want 120", len(rows))
	}
	if meta.Metered() {
		t.Error("/geo is uncounted")
	}
}

// A server that repeated a page would spin the walker forever. Stopping
// silently would look exactly like the end of the listing, so it is reported.
func TestGeoBrowseAllReportsAStalledWalkRatherThanLooping(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, geoPageBody(1000, 100, 1)) // always page 1
	})
	err := rec.client.GeoBrowseAll(context.Background(), GeoParams{}, func(*GeoResponse) error { return nil })
	if !errors.Is(err, ErrPagingStalled) {
		t.Fatalf("err = %v, want ErrPagingStalled", err)
	}
}

func TestGeoBrowseAllStopsWhenTheHandlerFails(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		freeHeaders(w)
		writeJSON(w, 200, geoPageBody(1000, limit, page))
	})
	sentinel := errors.New("stop here")
	err := rec.client.GeoBrowseAll(context.Background(), GeoParams{}, func(*GeoResponse) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the handler's error", err)
	}
	if rec.hits() != 1 {
		t.Errorf("hits = %d, want 1", rec.hits())
	}
}

func TestGeoBrowseRefusesALimitAboveTheCap(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})
	_, err := rec.client.GeoBrowse(context.Background(), GeoParams{Limit: GeoMaxLimit + 1})
	var valErr *ValidationError
	if !errors.As(err, &valErr) || valErr.Parameter != "limit" {
		t.Fatalf("err = %v, want a local limit refusal", err)
	}
}

// ── model 2: /geo/search, no paging at all ───────────────────────────────────

// ⚠️ --all on search is meaningless. It must SAY so rather than quietly hand
// back one page under an "everything" banner.
func TestGeoSearchAllRefusesInsteadOfPretendingToPage(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a search walk must not send any request")
	})
	err := rec.client.GeoSearchAll(context.Background(), SearchParams{Q: "valencia"},
		func(*SearchResponse) error { return nil })
	if !errors.Is(err, ErrNoPaging) {
		t.Fatalf("err = %v, want ErrNoPaging", err)
	}
	if !strings.Contains(err.Error(), "50") {
		t.Errorf("the message should name the server's clamp: %v", err)
	}
	if !strings.Contains(err.Error(), "Narrow the query") {
		t.Errorf("the message should say what to do instead: %v", err)
	}
	if rec.hits() != 0 {
		t.Errorf("hits = %d, want 0", rec.hits())
	}
}

// The server clamps a large limit rather than refusing it, so a caller asking
// for 1000 gets 200 OK and 50 rows. The client must not pretend otherwise.
func TestGeoSearchPassesTheLimitThroughAndTheServerClamps(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		rows := make([]string, SearchServerClamp)
		for i := range rows {
			rows[i] = fmt.Sprintf(`{"kind":"zone","geo_id":"R%d","name":"Place %d","level":"city",`+
				`"country":"es","ancestors":[],"h3_res":8,"hexes_url":"/x","prices_available":true,`+
				`"current_period":"2026-08-01"}`, i, i)
		}
		writeJSON(w, 200, `{"query":"valencia","results":[`+strings.Join(rows, ",")+`]}`)
	})

	res, err := rec.client.GeoSearch(context.Background(), SearchParams{Q: "valencia", Limit: 1000})
	if err != nil {
		t.Fatalf("GeoSearch: %v", err)
	}
	if !strings.Contains(rec.last().Query, "limit=1000") {
		t.Errorf("the limit must go on the wire as asked: %q", rec.last().Query)
	}
	if len(res.Results) != SearchServerClamp {
		t.Errorf("results = %d, want the server's clamp of %d", len(res.Results), SearchServerClamp)
	}
}

// ── model 3: /geo/{geo_id}/hexes, keyset cursor ──────────────────────────────

func TestPlaceHexesAllFollowsTheCursorToNull(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		switch r.URL.Query().Get("cursor") {
		case "":
			writeJSON(w, 200, hexPageBody([]string{"1", "2"}, `"cursor-a"`))
		case "cursor-a":
			writeJSON(w, 200, hexPageBody([]string{"3", "4"}, `"cursor-b"`))
		case "cursor-b":
			writeJSON(w, 200, hexPageBody([]string{"5"}, `null`))
		default:
			writeJSON(w, 400, `{"error":"invalid_cursor","parameter":"cursor","message":"not ours","docs_url":"d"}`)
		}
	})

	hexes, meta, err := rec.client.PlaceHexesCollect(context.Background(), "R14727511", HexesParams{})
	if err != nil {
		t.Fatalf("PlaceHexesCollect: %v", err)
	}
	if got := strings.Join(hexes, ","); got != "1,2,3,4,5" {
		t.Errorf("hexes = %q", got)
	}
	if rec.hits() != 3 {
		t.Errorf("hits = %d, want 3 — a null next_cursor ends the listing", rec.hits())
	}
	if meta.Metered() {
		t.Error("the hex listing is uncounted")
	}
	// The cursor goes back verbatim, and every other parameter stays put —
	// that is what keeps it valid.
	if q := rec.requests[1].Query; !strings.Contains(q, "cursor=cursor-a") {
		t.Errorf("second request query = %q", q)
	}
}

// ⚠️ A cursor is scoped to the exact query that issued it. Starting a full
// walk from someone else's position is refused locally: it would either be an
// invalid_cursor or, worse, a partial listing that looks complete.
func TestPlaceHexesAllRefusesACallerSuppliedCursor(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})
	err := rec.client.PlaceHexesAll(context.Background(), "R14727511",
		HexesParams{Cursor: "issued-for-valencia"}, func(*HexPage) error { return nil })
	var valErr *ValidationError
	if !errors.As(err, &valErr) || valErr.Parameter != "cursor" {
		t.Fatalf("err = %v, want a local cursor refusal", err)
	}
	if rec.hits() != 0 {
		t.Errorf("hits = %d, want 0", rec.hits())
	}
}

// A cursor issued for another place is a 400 invalid_cursor, and it must not
// be retried: the same cursor never becomes valid.
func TestACursorFromAnotherQueryIsRejectedAndNotRetried(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 400, `{"error":"invalid_cursor","parameter":"cursor",`+
			`"message":"That cursor was issued for a different query. Start the listing again without a cursor.",`+
			`"docs_url":"https://docs.investviews.ai/errors.html"}`)
	})

	_, err := rec.client.PlaceHexes(context.Background(), "R349053", HexesParams{Cursor: "valencias-cursor"})
	var cursorErr *InvalidCursorError
	if !errors.As(err, &cursorErr) {
		t.Fatalf("err = %T, want *InvalidCursorError", err)
	}
	if rec.hits() != 1 {
		t.Errorf("hits = %d, want 1", rec.hits())
	}
}

func TestPlaceHexesRefusesALimitAboveTheCap(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this request should never have been sent")
	})
	_, err := rec.client.PlaceHexes(context.Background(), "R1", HexesParams{Limit: HexesMaxLimit + 1})
	var valErr *ValidationError
	if !errors.As(err, &valErr) || valErr.Parameter != "limit" {
		t.Fatalf("err = %v, want a local limit refusal", err)
	}
}

// ── fixtures ─────────────────────────────────────────────────────────────────

// geoPageBody renders one offset page and NOTHING ELSE: no total, no has_more,
// no page echo — exactly what the live API carries.
func geoPageBody(total, limit, page int) string {
	if limit <= 0 {
		limit = GeoDefaultLimit
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, fmt.Sprintf(
			`{"kind":"zone","geo_id":"R%d","name":"Place %d","level":"city","country":"es",`+
				`"ancestors":[{"geo_id":"es","name":"ES","level":"country"}],"h3_res":8,`+
				`"hexes_url":"/public/v1/geo/R%d/hexes","prices_available":true,`+
				`"current_period":"2026-08-01","reports_available":true}`, i, i, i))
	}
	return `{"parent":{"geo_id":"es","name":"ES","level":"country","country":"es","ancestors":[]},` +
		`"results":[` + strings.Join(rows, ",") + `]}`
}

func hexPageBody(hexes []string, nextCursor string) string {
	quoted := make([]string, len(hexes))
	for i, h := range hexes {
		quoted[i] = `"` + h + `"`
	}
	return `{"geo_id":"R14727511","name":"Russafa","level":"microzone","country":"es","h3_res":8,` +
		`"hexes":[` + strings.Join(quoted, ",") + `],"next_cursor":` + nextCursor + `,` +
		`"prices_available":true,"reports_available":true}`
}

// ⚠️ A PAGE LONGER THAN THE LIMIT MEANS THE SERVER IGNORED THE LIMIT, AND AN
// ENDPOINT THAT IGNORES THE LIMIT IS NOT PAGING.
//
// /geo does this at its root: `?limit=10&page=1|2|3` each answer with all 37
// countries. The walker used to see 37 >= 10, ask for page 2, get the same 37
// ids back and be caught by the stall guard — so `geo browse --all --limit 10`
// exited 1 on a perfectly well-formed query. The guard was right; the walker
// should never have needed it.
func TestGeoBrowseAllStopsWhenTheServerIgnoresTheLimit(t *testing.T) {
	const ignoredLimit = 37
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		// Whatever limit or page is asked for, answer with all 37 rows —
		// the measured behaviour of /geo with no parent.
		writeJSON(w, 200, geoPageBody(ignoredLimit, ignoredLimit, 1))
	})

	var pages int
	var rows []GeoResult
	err := rec.client.GeoBrowseAll(context.Background(), GeoParams{Limit: 10}, func(p *GeoResponse) error {
		pages++
		rows = append(rows, p.Results...)
		return nil
	})
	if err != nil {
		t.Fatalf("GeoBrowseAll: %v — a server that ignores the limit is not an error", err)
	}
	if errors.Is(err, ErrPagingStalled) {
		t.Fatal("the stall guard fired; the walker should have stopped before reaching it")
	}
	if pages != 1 {
		t.Errorf("pages = %d, want 1 — there is no page 2 to ask for", pages)
	}
	if len(rows) != ignoredLimit {
		t.Errorf("rows = %d, want %d — every row is still handed to the caller", len(rows), ignoredLimit)
	}
	if rec.hits() != 1 {
		t.Errorf("hits = %d, want 1 — a second request would fetch the same rows again", rec.hits())
	}
}

// The stall guard stays, and still fires on the case it was built for: a
// server that repeats a FULL page, which is not "the limit was ignored" and
// would otherwise spin forever. Pinned so the rule above cannot be widened
// into swallowing it.
func TestGeoBrowseAllStillReportsARepeatedFullPage(t *testing.T) {
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, geoPageBody(1000, 20, 1)) // always page 1, exactly the limit
	})
	err := rec.client.GeoBrowseAll(context.Background(), GeoParams{Limit: 20}, func(*GeoResponse) error { return nil })
	if !errors.Is(err, ErrPagingStalled) {
		t.Fatalf("err = %v, want ErrPagingStalled", err)
	}
}

// The other two models must not have been touched by the offset rule.
// /geo/{geo_id}/hexes ends on a null cursor, not on a page size — a server
// that returns more cells than the limit asked for is still paging, because
// the cursor says so.
func TestPlaceHexesAllIsUnaffectedByTheOffsetPageSizeRule(t *testing.T) {
	pages := []string{
		`{"geo_id":"R1","name":"P","level":"city","country":"es","h3_res":8,
		  "hexes":["613498076398616575","613498076398616576","613498076398616577"],
		  "next_cursor":"c1","prices_available":true,"reports_available":true}`,
		`{"geo_id":"R1","name":"P","level":"city","country":"es","h3_res":8,
		  "hexes":["613498076578971647"],"next_cursor":null,
		  "prices_available":true,"reports_available":true}`,
	}
	var n int
	rec := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		freeHeaders(w)
		writeJSON(w, 200, pages[n])
		n++
	})
	// limit 2, but the first page answers with 3 and a cursor: the cursor
	// is the end signal here, so the walk must continue.
	cells, _, err := rec.client.PlaceHexesCollect(context.Background(), "R1", HexesParams{Limit: 2})
	if err != nil {
		t.Fatalf("PlaceHexesCollect: %v", err)
	}
	if len(cells) != 4 {
		t.Errorf("cells = %d, want 4 — a cursor walk ends on a null cursor, never on a page size", len(cells))
	}
}
